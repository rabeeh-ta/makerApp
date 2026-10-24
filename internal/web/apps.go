package web

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/games"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

// Apps are what a board lists in its Apps drawer. Your own apps are named,
// ordered sets of screens made here. The public list holds makerApp's own apps
// (engine.Catalog, games included) and apps people publish; anyone can
// install those, and every board that does counts as an install.

var cycleOpts = []int{0, 10, 20, 30, 60, 300}

type appCard struct {
	*store.App
	ScreenCount int
	Installs    int
	Preview     *render.Screen
	On          []*store.Device // devices it's installed on
}

// listing is an app in the public list: makerApp's own, or one someone published.
type listing struct {
	ID, Name, Desc, By string
	Installs           int
	Preview            *render.Screen
	On                 []*store.Device // the user's devices it's installed on
}

// listings is the public list, most installed first.
func (s *Server) listings(r *http.Request, on map[string][]*store.Device) []listing {
	counts, _ := s.st.InstallCounts(r.Context())
	var out []listing
	for _, a := range engine.Catalog {
		p := s.eng.CatalogPreview(a.ID)
		out = append(out, listing{ID: a.ID, Name: a.Name, Desc: a.Desc, By: a.Publisher, Installs: counts[a.ID],
			Preview: &p, On: on[a.ID]})
	}
	published, _ := s.st.PublishedApps(r.Context())
	for _, p := range published {
		l := listing{ID: p.ID, Name: p.Name, Desc: p.Description, By: p.Publisher, Installs: counts[p.ID], On: on[p.ID]}
		if screens, _ := s.st.AppScreens(r.Context(), p.ID); len(screens) > 0 {
			pv := s.eng.PreviewScreen(r.Context(), screens[0], nil)
			l.Preview = &pv
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Installs > out[j].Installs })
	return out
}

func (s *Server) appsPage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	apps, err := s.st.UserApps(r.Context(), u.ID)
	if notFound(w, err) {
		return
	}
	devices, _ := s.st.UserDevices(r.Context(), u.ID)
	on := s.installedOn(r, devices)
	counts, _ := s.st.InstallCounts(r.Context())
	cards := make([]appCard, 0, len(apps))
	for _, a := range apps {
		screens, _ := s.st.AppScreens(r.Context(), a.ID)
		c := appCard{App: a, ScreenCount: len(screens), Installs: counts[a.ID], On: on[a.ID]}
		if len(screens) > 0 {
			p := s.eng.PreviewScreen(r.Context(), screens[0], nil)
			c.Preview = &p
		}
		cards = append(cards, c)
	}
	s.render(w, r, "apps", map[string]any{"Apps": cards, "Public": s.listings(r, on), "Devices": devices,
		"Username": u.Username})
}

// installedOn maps app IDs to the devices they're installed on.
func (s *Server) installedOn(r *http.Request, devices []*store.Device) map[string][]*store.Device {
	out := map[string][]*store.Device{}
	for _, d := range devices {
		ids, _ := s.st.DeviceAppIDs(r.Context(), d.ID)
		for _, id := range ids {
			out[id] = append(out[id], d)
		}
	}
	return out
}

func (s *Server) newApp(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "My app"
	}
	a := &store.App{UserID: u.ID, Name: name}
	if err := s.st.SaveApp(r.Context(), a); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	// Made from a device page: install it there right away.
	if d, err := s.st.UserDevice(r.Context(), u.ID, r.FormValue("device")); err == nil {
		if err := s.st.InstallApp(r.Context(), d.ID, a.ID); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		s.eng.PushDevice(r.Context(), d.ID)
	}
	http.Redirect(w, r, "/apps/"+a.ID, http.StatusSeeOther)
}

func (s *Server) loadApp(w http.ResponseWriter, r *http.Request) (*store.App, bool) {
	a, err := s.st.UserApp(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if notFound(w, err) {
		return nil, false
	}
	return a, true
}

type deviceInstall struct {
	*store.Device
	Installed bool
	Online    bool
}

func (s *Server) appPage(w http.ResponseWriter, r *http.Request) {
	if a, ok := s.loadApp(w, r); ok {
		s.renderApp(w, r, a, "")
	}
}

func (s *Server) renderApp(w http.ResponseWriter, r *http.Request, a *store.App, errMsg string) {
	screens, _ := s.st.AppScreens(r.Context(), a.ID)
	// Sources whose data anyone who installs the app would see.
	var sources []string
	for _, sc := range screens {
		if src, err := s.st.UserSource(r.Context(), a.UserID, sc.SourceID); err == nil && !slices.Contains(sources, src.Name) {
			sources = append(sources, src.Name)
		}
	}
	counts, _ := s.st.InstallCounts(r.Context())
	cards := make([]screenCard, len(screens))
	for i, sc := range s.eng.PreviewScreens(r.Context(), screens) {
		cards[i] = screenCard{S: sc, First: i == 0, Last: i == len(screens)-1}
	}
	devices, _ := s.st.UserDevices(r.Context(), a.UserID)
	on, _ := s.st.DevicesWithApp(r.Context(), a.ID)
	installs := make([]deviceInstall, len(devices))
	for i, d := range devices {
		installs[i] = deviceInstall{Device: d, Installed: slices.Contains(on, d.ID), Online: s.eng.Online(d.ID)}
	}
	s.render(w, r, "app", map[string]any{
		"App":       a,
		"Screens":   cards,
		"Devices":   installs,
		"CycleOpts": cycleOpts,
		"Saved":     r.URL.Query().Get("saved"),
		"Error":     errMsg,
		"Username":  currentUser(r).Username,
		"Installs":  counts[a.ID],
		"Sources":   sources,
	})
}

var usernameRe = regexp.MustCompile(`^[a-z0-9_-]{3,20}$`)

// Names people can't take, so nobody poses as makerApp.
var reservedNames = []string{"makerinc", "makerapp", "maker", "esphog", "admin", "support", "official"}

// publishApp makes the app public right away: anyone can find and install it.
// The first time, the user also picks the public name it's shown under.
func (s *Server) publishApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	a.Description = strings.TrimSpace(r.FormValue("description"))
	if u.Username == "" {
		name := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
		if !usernameRe.MatchString(name) || slices.Contains(reservedNames, name) {
			s.renderApp(w, r, a, "Pick a public name of 3 to 20 letters, digits, - or _.")
			return
		}
		if err := s.st.SetUsername(r.Context(), u.ID, name); errors.Is(err, store.ErrTaken) {
			s.renderApp(w, r, a, "@"+name+" is taken. Try another name.")
			return
		} else if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
	}
	if screens, _ := s.st.AppScreens(r.Context(), a.ID); len(screens) == 0 {
		s.renderApp(w, r, a, "Add a screen before publishing.")
		return
	}
	if a.Description == "" {
		s.renderApp(w, r, a, "Add a short description, so people know what it shows.")
		return
	}
	if len(a.Description) > 140 {
		a.Description = a.Description[:140]
	}
	a.PublishedAt = time.Now().Unix()
	if err := s.st.SaveApp(r.Context(), a); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/apps/"+a.ID+"?saved=published", http.StatusSeeOther)
}

// unpublishApp takes it out of the public list and off other people's boards.
func (s *Server) unpublishApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	devices, err := s.st.Unpublish(r.Context(), a.UserID, a.ID)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	for _, d := range devices {
		s.eng.PushDevice(r.Context(), d)
	}
	http.Redirect(w, r, "/apps/"+a.ID+"?saved=unpublished", http.StatusSeeOther)
}

func (s *Server) saveApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		a.Name = name
	}
	a.CycleSec = clamp(atoi(r.FormValue("cycle_sec"), a.CycleSec), 0, 3600)
	if err := s.st.SaveApp(r.Context(), a); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.eng.PushApp(r.Context(), a.ID)
	http.Redirect(w, r, "/apps/"+a.ID+"?saved=1", http.StatusSeeOther)
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	devices, _ := s.st.DevicesWithApp(r.Context(), a.ID)
	if err := s.st.DeleteApp(r.Context(), a.UserID, a.ID); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	for _, d := range devices {
		s.eng.PushDevice(r.Context(), d)
	}
	http.Redirect(w, r, "/apps", http.StatusSeeOther)
}

func (s *Server) moveAppScreen(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	if err := s.st.MoveAppScreen(r.Context(), a.ID, r.PathValue("sid"), delta); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.eng.PushApp(r.Context(), a.ID)
	http.Redirect(w, r, "/apps/"+a.ID, http.StatusSeeOther)
}

func (s *Server) installApp(w http.ResponseWriter, r *http.Request)   { s.setInstalled(w, r, true) }
func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) { s.setInstalled(w, r, false) }

// setInstalled installs or removes the app in the path on the device in the
// form, then goes back to the page the form came from.
func (s *Server) setInstalled(w http.ResponseWriter, r *http.Request, on bool) {
	u := currentUser(r)
	d, err := s.st.UserDevice(r.Context(), u.ID, r.FormValue("device"))
	if notFound(w, err) {
		return
	}
	id := r.PathValue("id")
	if _, ok := engine.CatalogEntry(id); !ok && on {
		if _, _, err := s.st.InstallableApp(r.Context(), u.ID, id); notFound(w, err) {
			return
		}
	}
	if on {
		err = s.st.InstallApp(r.Context(), d.ID, id)
	} else {
		err = s.st.UninstallApp(r.Context(), d.ID, id)
	}
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.eng.PushDevice(r.Context(), d.ID)
	http.Redirect(w, r, backTo(r, "/devices/"+d.ID), http.StatusSeeOther)
}

// backTo is the form's "back" path when it stays on this site, else fallback.
func backTo(r *http.Request, fallback string) string {
	b := r.FormValue("back")
	if strings.HasPrefix(b, "/") && !strings.HasPrefix(b, "//") && !strings.HasPrefix(b, "/\\") {
		return b
	}
	return fallback
}

// gameScript serves a downloadable game to boards. The URL names the version,
// so its content never changes and can be cached for good.
func (s *Server) gameScript(w http.ResponseWriter, r *http.Request) {
	hash, ok := strings.CutSuffix(r.PathValue("file"), ".lua")
	src, found := games.Script(r.PathValue("id"), hash)
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(src)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(src)
	s.log.Info("game sent", "game", r.PathValue("id"), "version", hash)
}
