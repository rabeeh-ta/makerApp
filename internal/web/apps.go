package web

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"makerapp/internal/games"
	"makerapp/internal/library"
	"makerapp/internal/store"
)

// Apps are what a board lists in its Apps drawer. Your own apps are named,
// ordered sets of screens made here; the App Store (store.go) lists the ones
// anyone can install.

var cycleOpts = []int{0, 5, 10, 20, 30, 60, 300}

type appCard struct {
	*store.App
	ScreenCount int
	Installs    int
	Preview     *webScreen
	On          []*store.Device // devices it's installed on
}

func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
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
			c.Preview = toWeb(s.eng.PreviewScreen(r.Context(), screens[0], nil))
		}
		cards = append(cards, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"Apps": cards, "Username": u.Username})
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
	// Made from a device page: installed there right away.
	a, err := s.lib.CreateApp(r.Context(), currentUser(r).ID, r.FormValue("name"), r.FormValue("device"), "")
	if failed(w, err) {
		return
	}
	redirect(w, "/apps/"+a.ID)
}

func (s *Server) loadApp(w http.ResponseWriter, r *http.Request) (*store.App, bool) {
	a, err := s.st.UserApp(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if notFound(w, err) {
		return nil, false
	}
	return a, true
}

// appSource is a source an app's screens use.
type appSource struct {
	ID, Name   string
	Personal   bool
	HasExample bool
}

type deviceInstall struct {
	*store.Device
	Installed bool
	Online    bool
}

func (s *Server) app(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	screens, _ := s.st.AppScreens(r.Context(), a.ID)
	// The sources its screens use, for the publish page: a shared source's
	// data shows on the boards of everyone who installs the app; everyone
	// sends their own to a personal one.
	var sources []appSource
	seen := map[string]bool{}
	for _, sc := range screens {
		if seen[sc.SourceID] {
			continue
		}
		seen[sc.SourceID] = true
		if src, err := s.st.UserSource(r.Context(), a.UserID, sc.SourceID); err == nil {
			sources = append(sources, appSource{ID: src.ID, Name: src.Name, Personal: src.PerUser, HasExample: src.Example != ""})
		}
	}
	counts, _ := s.st.InstallCounts(r.Context())
	cards := make([]screenCard, len(screens))
	for i, sc := range s.eng.PreviewScreens(r.Context(), screens) {
		cards[i] = screenCard{S: toWeb(sc), First: i == 0, Last: i == len(screens)-1}
	}
	devices, _ := s.st.UserDevices(r.Context(), a.UserID)
	on, _ := s.st.DevicesWithApp(r.Context(), a.ID)
	installs := make([]deviceInstall, len(devices))
	for i, d := range devices {
		installs[i] = deviceInstall{Device: d, Installed: slices.Contains(on, d.ID), Online: s.eng.Online(d.ID)}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"App":       a,
		"Screens":   cards,
		"Devices":   installs,
		"CycleOpts": cycleOpts,
		"Username":  currentUser(r).Username,
		"Installs":  counts[a.ID],
		"Sources":   sources,
	})
}

// maxSetup is how long an app's README can be, in characters: room for a
// short script in a code block.
const maxSetup = 20000

var usernameRe = regexp.MustCompile(`^[a-z0-9_-]{3,20}$`)

// Names people can't take, so nobody poses as makerApp.
var reservedNames = []string{"makerinc", "makerapp", "maker", "esphog", "admin", "support", "official"}

// publishApp makes the app public right away: anyone can find and install it
// in the App Store. The first time, the user also picks the public name it's
// shown under. Posting again saves its description and README.
func (s *Server) publishApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	a.Description = strings.TrimSpace(r.FormValue("description"))
	a.Setup = library.Limit(strings.TrimSpace(strings.ReplaceAll(r.FormValue("setup"), "\r\n", "\n")), maxSetup)
	if u.Username == "" {
		name := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
		if !usernameRe.MatchString(name) || slices.Contains(reservedNames, name) {
			fail(w, http.StatusBadRequest, "Pick a public name of 3 to 20 letters, digits, - or _.")
			return
		}
		if err := s.st.SetUsername(r.Context(), u.ID, name); errors.Is(err, store.ErrTaken) {
			fail(w, http.StatusBadRequest, "@"+name+" is taken. Try another name.")
			return
		} else if err != nil {
			fail(w, http.StatusInternalServerError, "server error")
			return
		}
	}
	if screens, _ := s.st.AppScreens(r.Context(), a.ID); len(screens) == 0 {
		fail(w, http.StatusBadRequest, "Add a screen before publishing.")
		return
	}
	if a.Description == "" {
		fail(w, http.StatusBadRequest, "Add a short description, so people know what it shows.")
		return
	}
	a.Description = library.Limit(a.Description, 140)
	note := "store"
	if a.PublishedAt == 0 {
		a.PublishedAt = time.Now().Unix()
		note = "published"
	}
	if err := s.st.SaveApp(r.Context(), a); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	redirect(w, "/apps/"+a.ID+"?saved="+note)
}

// unpublishApp takes it out of the public list and off other people's boards.
func (s *Server) unpublishApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	devices, err := s.st.Unpublish(r.Context(), a.UserID, a.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	for _, d := range devices {
		s.eng.PushDevice(r.Context(), d)
	}
	redirect(w, "/apps/"+a.ID+"?saved=unpublished")
}

func (s *Server) saveApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	name, cycle := r.FormValue("name"), atoi(r.FormValue("cycle_sec"), a.CycleSec)
	if _, err := s.lib.UpdateApp(r.Context(), a.UserID, a.ID, library.AppChanges{Name: &name, CycleSec: &cycle}); failed(w, err) {
		return
	}
	redirect(w, "/apps/"+a.ID+"?saved=1")
}

func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	a, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	devices, _ := s.st.DevicesWithApp(r.Context(), a.ID)
	if err := s.st.DeleteApp(r.Context(), a.UserID, a.ID); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	for _, d := range devices {
		s.eng.PushDevice(r.Context(), d)
	}
	redirect(w, "/apps")
}

func (s *Server) moveAppScreen(w http.ResponseWriter, r *http.Request) {
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	appID := r.PathValue("id")
	if failed(w, s.lib.MoveScreen(r.Context(), currentUser(r).ID, appID, r.PathValue("sid"), delta)) {
		return
	}
	redirect(w, "/apps/"+appID)
}

func (s *Server) installApp(w http.ResponseWriter, r *http.Request)   { s.setInstalled(w, r, true) }
func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) { s.setInstalled(w, r, false) }

// setInstalled installs or removes the app in the path on the device in the
// form, then goes back to the page the form came from.
func (s *Server) setInstalled(w http.ResponseWriter, r *http.Request, on bool) {
	d, err := s.lib.SetInstalled(r.Context(), currentUser(r).ID, r.FormValue("device"), r.PathValue("id"), on, false)
	if failed(w, err) {
		return
	}
	redirect(w, backTo(r, "/devices/"+d.ID))
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
