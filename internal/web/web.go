// Package web is the makerApp server's HTTP side: the device-facing endpoints
// (WebSocket, firmware, flashing, games, push) and the JSON API behind the
// React website, which is served from Config.Frontend.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/limit"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

type Config struct {
	PublicURL     string // e.g. https://maker.app.rabeeta.xyz; derived from the request when empty
	AllowSignup   bool
	SecureCookies bool
	Frontend      http.Handler  // the website; serves every path not claimed here
	MCP           http.Handler  // the MCP connector; served at /mcp to signed-in AI clients
	Fetch         *fetch.Client // outbound requests: MCP clients' metadata documents
}

type Server struct {
	cfg Config
	st  *store.Store
	eng *engine.Engine
	lib *library.Library
	log *slog.Logger
	lim *limit.Limiter // login, signup and pairing attempts
}

func New(cfg Config, st *store.Store, eng *engine.Engine, lib *library.Library, log *slog.Logger) *Server {
	if cfg.Frontend == nil {
		cfg.Frontend = http.NotFoundHandler()
	}
	return &Server{cfg: cfg, st: st, eng: eng, lib: lib, log: log, lim: limit.New()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Device-facing: flashed boards and users' scripts depend on these exact routes.
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
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/signup", s.signup)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/account", s.auth(s.account))
	mux.HandleFunc("POST /api/account/password", s.auth(s.changePassword))

	// Flashing and pairing
	mux.HandleFunc("GET /api/flash", s.auth(s.flashInfo))
	mux.HandleFunc("POST /api/flash/claims", s.auth(s.newClaim))
	mux.HandleFunc("GET /api/flash/status", s.auth(s.flashStatus))
	mux.HandleFunc("POST /api/pair", s.auth(s.pair))
	mux.HandleFunc("POST /api/pair/try", s.auth(s.pairTry))

	// Devices
	mux.HandleFunc("GET /api/devices", s.auth(s.devices))
	mux.HandleFunc("GET /api/devices/{id}", s.auth(s.device))
	mux.HandleFunc("GET /api/devices/{id}/status", s.auth(s.deviceStatus))
	mux.HandleFunc("POST /api/devices/{id}", s.auth(s.saveDevice))
	mux.HandleFunc("POST /api/devices/{id}/do/{action}", s.auth(s.deviceAction))
	mux.HandleFunc("POST /api/devices/{id}/apps/{app}/move", s.auth(s.moveDeviceApp))
	mux.HandleFunc("GET /api/devices/{id}/screensaver", s.auth(s.screensaver))
	mux.HandleFunc("POST /api/devices/{id}/screensaver", s.auth(s.saveScreensaver))

	// Data sources
	mux.HandleFunc("GET /api/sources", s.auth(s.sources))
	mux.HandleFunc("GET /api/sources/new", s.auth(s.sourceEdit))
	mux.HandleFunc("POST /api/sources", s.auth(s.saveSource))
	mux.HandleFunc("GET /api/sources/{id}", s.auth(s.sourceEdit))
	mux.HandleFunc("POST /api/sources/{id}", s.auth(s.saveSource))
	mux.HandleFunc("POST /api/sources/{id}/fetch", s.auth(s.fetchSource))
	mux.HandleFunc("POST /api/sources/{id}/delete", s.auth(s.deleteSource))

	// Apps
	mux.HandleFunc("GET /api/apps", s.auth(s.apps))
	mux.HandleFunc("POST /api/apps", s.auth(s.newApp))
	mux.HandleFunc("GET /api/apps/{id}", s.auth(s.app))
	mux.HandleFunc("POST /api/apps/{id}", s.auth(s.saveApp))
	mux.HandleFunc("POST /api/apps/{id}/delete", s.auth(s.deleteApp))
	mux.HandleFunc("POST /api/apps/{id}/install", s.auth(s.installApp))
	mux.HandleFunc("POST /api/apps/{id}/uninstall", s.auth(s.uninstallApp))
	mux.HandleFunc("POST /api/apps/{id}/publish", s.auth(s.publishApp))
	mux.HandleFunc("POST /api/apps/{id}/unpublish", s.auth(s.unpublishApp))
	mux.HandleFunc("POST /api/apps/{id}/screens/{sid}/move", s.auth(s.moveAppScreen))

	// App Store
	mux.HandleFunc("GET /api/store", s.auth(s.storeList))
	mux.HandleFunc("GET /api/store/{id}", s.auth(s.storeApp))

	// Screens
	mux.HandleFunc("GET /api/screens/new", s.auth(s.screenEdit))
	mux.HandleFunc("POST /api/screens", s.auth(s.saveScreen))
	mux.HandleFunc("POST /api/screens/preview", s.auth(s.previewScreen))
	mux.HandleFunc("GET /api/screens/explorer", s.auth(s.explorer))
	mux.HandleFunc("GET /api/screens/{id}", s.auth(s.screenEdit))
	mux.HandleFunc("POST /api/screens/{id}", s.auth(s.saveScreen))
	mux.HandleFunc("POST /api/screens/{id}/delete", s.auth(s.deleteScreen))

	// The MCP connector and its sign-in (OAuth 2.1). /oauth/authorize itself is
	// a page of the website; it uses the /api/oauth endpoints.
	if s.cfg.MCP != nil {
		mux.HandleFunc("/mcp", s.mcp)
	}
	mux.HandleFunc("/.well-known/oauth-protected-resource", publicCORS(s.resourceMetadata))
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", publicCORS(s.resourceMetadata))
	mux.HandleFunc("/.well-known/oauth-authorization-server", publicCORS(s.authServerMetadata))
	mux.HandleFunc("POST /oauth/register", publicCORS(s.register))
	mux.HandleFunc("POST /oauth/token", publicCORS(s.token))
	mux.HandleFunc("OPTIONS /oauth/", publicCORS(nil))
	mux.HandleFunc("GET /api/oauth/authorize", s.auth(s.authorizeInfo))
	mux.HandleFunc("POST /api/oauth/authorize", s.auth(s.authorize))
	mux.HandleFunc("GET /api/oauth/grants", s.auth(s.grants))
	mux.HandleFunc("POST /api/oauth/grants/{id}/revoke", s.auth(s.revokeGrant))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })
	// Screens live inside apps now; old links still land somewhere useful.
	mux.HandleFunc("GET /screens", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/apps", http.StatusSeeOther) })
	mux.Handle("/", s.cfg.Frontend)

	return s.logRequests(s.checkOrigin(mux))
}

// ---- JSON responses ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// redirect answers a form post that worked with the page the website shows next.
func redirect(w http.ResponseWriter, to string) {
	writeJSON(w, http.StatusOK, map[string]string{"redirect": to})
}

// fail answers with a message the website shows to the user.
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// failed answers a library error: a user's mistake (400 with its message), a
// missing record (404) or a server failure (500). It reports whether err was set.
func failed(w http.ResponseWriter, err error) bool {
	if e, ok := library.IsUserError(err); ok {
		fail(w, http.StatusBadRequest, e.Msg)
		return true
	}
	return notFound(w, err)
}

func notFound(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return true
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return true
	}
	return false
}

// webScreen is a screen as the website shows it: with the name and the source
// error, which the device protocol leaves out.
type webScreen struct {
	render.Screen
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

func toWeb(sc render.Screen) *webScreen {
	return &webScreen{Screen: sc, Name: sc.Name, Error: sc.Error}
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
			fail(w, http.StatusUnauthorized, "sign in required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

// checkOrigin rejects cross-site posts (CSRF), which only matter for requests
// a browser sends with the session cookie. Push, OAuth and MCP endpoints are
// exempt: scripts and AI clients call them with tokens, not cookies.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Method == http.MethodPost && !strings.HasPrefix(p, "/push/") && !strings.HasPrefix(p, "/oauth/") && p != "/mcp" {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					fail(w, http.StatusForbidden, "cross-site request blocked")
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
		if p := r.URL.Path; strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/oauth/") || p == "/mcp" {
			s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
		}
	})
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
