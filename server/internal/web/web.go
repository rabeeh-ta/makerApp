// Package web is the espHog website: accounts, browser flashing, pairing,
// data sources and the screen builder. Pages are Go templates enhanced with
// htmx, all embedded in the binary.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"esphog/internal/engine"
	"esphog/internal/render"
	"esphog/internal/store"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Config struct {
	PublicURL     string // e.g. https://maker.rabeeta.xyz; derived from the request when empty
	AllowSignup   bool
	SecureCookies bool
}

type Server struct {
	cfg   Config
	st    *store.Store
	eng   *engine.Engine
	log   *slog.Logger
	pages map[string]*template.Template

	limiterMu sync.Mutex
	attempts  map[string][]time.Time
}

func New(cfg Config, st *store.Store, eng *engine.Engine, log *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, st: st, eng: eng, log: log, pages: map[string]*template.Template{},
		attempts: map[string][]time.Time{}}
	entries, err := fs.Glob(templateFS, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	for _, p := range entries {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "templates/pages/"), ".html")
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/partials/*.html", p)
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", name, err)
		}
		s.pages[name] = t
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Device-facing
	mux.HandleFunc("GET /ws", s.eng.HandleWS)
	mux.HandleFunc("GET /fw/firmware.bin", s.otaFirmware)
	mux.HandleFunc("GET /fw/{hash}/firmware.bin", s.otaFirmware)
	mux.HandleFunc("GET /flash/manifest.json", s.flashManifest)
	mux.HandleFunc("GET /flash/config.bin", s.flashConfig)
	mux.HandleFunc("GET /flash/{part}", s.flashPart)
	mux.HandleFunc("GET /flash/{hash}/{part}", s.flashPart)
	mux.HandleFunc("POST /push/{token}", s.pushData)
	mux.HandleFunc("GET /games/{id}/{file}", s.gameScript)

	// Accounts
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /signup", s.signupPage)
	mux.HandleFunc("POST /signup", s.signup)
	mux.HandleFunc("POST /logout", s.logout)

	// App
	mux.HandleFunc("GET /flash", s.auth(s.flashPage))
	mux.HandleFunc("GET /flash/status", s.auth(s.flashStatus))
	mux.HandleFunc("GET /pair", s.auth(s.pairPage))
	mux.HandleFunc("POST /pair", s.auth(s.pair))
	mux.HandleFunc("GET /pair/status", s.auth(s.pairStatus))

	mux.HandleFunc("GET /devices", s.auth(s.devicesPage))
	mux.HandleFunc("GET /devices/list", s.auth(s.devicesList))
	mux.HandleFunc("GET /devices/{id}", s.auth(s.devicePage))
	mux.HandleFunc("GET /devices/{id}/status", s.auth(s.deviceStatus))
	mux.HandleFunc("POST /devices/{id}", s.auth(s.saveDevice))
	mux.HandleFunc("POST /devices/{id}/do/{action}", s.auth(s.deviceAction))
	mux.HandleFunc("GET /devices/{id}/settings", s.auth(s.deviceSettingsPage))
	mux.HandleFunc("POST /devices/{id}/apps/{app}/move", s.auth(s.moveDeviceApp))

	mux.HandleFunc("GET /sources", s.auth(s.sourcesPage))
	mux.HandleFunc("GET /sources/new", s.auth(s.sourceEditPage))
	mux.HandleFunc("POST /sources/new", s.auth(s.saveSource))
	mux.HandleFunc("GET /sources/{id}", s.auth(s.sourceEditPage))
	mux.HandleFunc("POST /sources/{id}", s.auth(s.saveSource))
	mux.HandleFunc("POST /sources/{id}/fetch", s.auth(s.fetchSource))
	mux.HandleFunc("POST /sources/{id}/delete", s.auth(s.deleteSource))

	mux.HandleFunc("GET /apps", s.auth(s.appsPage))
	mux.HandleFunc("POST /apps/new", s.auth(s.newApp))
	mux.HandleFunc("GET /apps/{id}", s.auth(s.appPage))
	mux.HandleFunc("POST /apps/{id}", s.auth(s.saveApp))
	mux.HandleFunc("POST /apps/{id}/delete", s.auth(s.deleteApp))
	mux.HandleFunc("POST /apps/{id}/install", s.auth(s.installApp))
	mux.HandleFunc("POST /apps/{id}/uninstall", s.auth(s.uninstallApp))
	mux.HandleFunc("POST /apps/{id}/publish", s.auth(s.publishApp))
	mux.HandleFunc("POST /apps/{id}/unpublish", s.auth(s.unpublishApp))
	mux.HandleFunc("POST /apps/{id}/screens/{sid}/move", s.auth(s.moveAppScreen))

	mux.HandleFunc("GET /screens", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/apps", http.StatusSeeOther) })
	mux.HandleFunc("GET /screens/new", s.auth(s.screenEditPage))
	mux.HandleFunc("POST /screens/new", s.auth(s.saveScreen))
	mux.HandleFunc("POST /screens/preview", s.auth(s.previewScreen))
	mux.HandleFunc("GET /screens/explorer", s.auth(s.explorer))
	mux.HandleFunc("GET /screens/{id}", s.auth(s.screenEditPage))
	mux.HandleFunc("POST /screens/{id}", s.auth(s.saveScreen))
	mux.HandleFunc("POST /screens/{id}/delete", s.auth(s.deleteScreen))

	return s.logRequests(s.checkOrigin(mux))
}

// ---- middleware ----

type ctxKey int

const userKey ctxKey = 0

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func (s *Server) userFromCookie(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.st.UserBySession(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return u
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.userFromCookie(r)
		if u == nil {
			if r.Header.Get("HX-Request") != "" {
				w.Header().Set("HX-Redirect", "/login")
				return
			}
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

// checkOrigin rejects cross-site form posts (CSRF). Device and push endpoints are exempt.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !strings.HasPrefix(r.URL.Path, "/push/") {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-site request blocked", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/static/") && r.URL.Path != "/ws" {
			s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
		}
	})
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		h.ServeHTTP(w, r)
	})
}

// allow is a small per-key rate limiter for login and pairing attempts.
func (s *Server) allow(key string, max int, per time.Duration) bool {
	s.limiterMu.Lock()
	defer s.limiterMu.Unlock()
	cut := time.Now().Add(-per)
	recent := s.attempts[key][:0]
	for _, t := range s.attempts[key] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= max {
		s.attempts[key] = recent
		return false
	}
	s.attempts[key] = append(recent, time.Now())
	return true
}

// ---- rendering ----

// assetVersion changes whenever the embedded CSS/JS changes, so browsers and
// proxies never serve stale styles after a deploy.
var assetVersion = func() string {
	h := sha256.New()
	for _, f := range []string{"static/style.css", "static/app.js", "static/data.js"} {
		b, _ := staticFS.ReadFile(f)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

var funcs = template.FuncMap{
	"assetV":    func() string { return assetVersion },
	"hasPrefix": strings.HasPrefix,
	"ago": func(unix int64) string {
		if unix == 0 {
			return "never"
		}
		d := time.Since(time.Unix(unix, 0))
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 48*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		}
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	},
	"json": func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	},
	"add": func(a, b int) int { return a + b },
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
	"pctw": func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
	// count shortens big numbers: 999, 1.2k, 3.4M.
	"count": func(n int) string {
		switch {
		case n < 1000:
			return strconv.Itoa(n)
		case n < 1000000:
			return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
		}
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000), ".0") + "M"
	},
	"spark": sparkPoints,
	"lines": func(s string) []string { return strings.Split(s, "\n") },
	"contains": func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	},
	"layoutName": func(l string) string {
		return map[string]string{"big_number": "Big number", "list": "List", "text": "Text",
			"spark": "Sparkline", "progress": "Progress"}[l]
	},
	"layouts": func() []string { return render.Layouts },
	"cycleName": func(sec int) string {
		switch {
		case sec == 0:
			return "Off"
		case sec < 60:
			return fmt.Sprintf("Every %d seconds", sec)
		case sec == 60:
			return "Every minute"
		}
		return fmt.Sprintf("Every %d minutes", sec/60)
	},
	"list": func(v ...string) []string { return v },
	// hasArt: a catalog Lua app, which has a preview picture in partials/app_art.html.
	"hasArt": func(layout string) bool {
		a, ok := engine.CatalogEntry(layout)
		return ok && a.Lua != ""
	},
	"hasDevice": func(list []*store.Device, id string) bool {
		return slices.ContainsFunc(list, func(d *store.Device) bool { return d.ID == id })
	},
	"deviceFields": func() any { return render.DeviceFields },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	},
}

// sparkPoints maps a series to SVG polyline points in a 224x60 box.
func sparkPoints(series []float64) string {
	if len(series) < 2 {
		return ""
	}
	lo, hi := series[0], series[0]
	for _, v := range series {
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi-lo < 1e-9 {
		hi = lo + 1
	}
	var b strings.Builder
	for i, v := range series {
		x := float64(i) * 224 / float64(len(series)-1)
		y := 60 - (v-lo)/(hi-lo)*60
		fmt.Fprintf(&b, "%.1f,%.1f ", x, y)
	}
	return b.String()
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, set := data["User"]; !set {
		data["User"] = currentUser(r)
	}
	data["Path"] = r.URL.Path
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		s.log.Error("render", "page", page, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// partial renders a named block (used by htmx swaps).
func (s *Server) partial(w http.ResponseWriter, name string, data any) {
	var t *template.Template
	for _, p := range s.pages {
		t = p
		break
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("partial", "name", name, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func notFound(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return true
	}
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return true
	}
	return false
}

// ---- public URL ----

// publicBase is the URL devices and browsers should use to reach this server.
// When running on localhost it swaps in the LAN IP so a freshly flashed board
// on the same network can connect.
func (s *Server) publicBase(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		h, port = host, ""
	}
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		if ip := lanIP(); ip != "" {
			h = ip
		}
	}
	if port != "" {
		host = net.JoinHostPort(h, port)
	} else {
		host = h
	}
	return scheme + "://" + host
}

func wsURL(base string) string {
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/ws"
}

// lanIP picks this machine's address on the local network: a private IPv4 on
// a physical interface. VPN/Tailscale tunnels are skipped since a board on the
// home WiFi can't reach them.
func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	best := ""
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			// Prefer the usual WiFi/Ethernet names (en0 on macOS, wlan0/eth0 on Linux).
			if strings.HasPrefix(iface.Name, "en") || strings.HasPrefix(iface.Name, "wl") || strings.HasPrefix(iface.Name, "eth") {
				return ip.String()
			}
			if best == "" {
				best = ip.String()
			}
		}
	}
	return best
}
