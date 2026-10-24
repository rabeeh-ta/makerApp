package web

import (
	"fmt"
	"net/http"

	"makerapp/internal/engine"
	"makerapp/internal/fw"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

// The device page is the hub: the apps installed on it, and Settings.

func (s *Server) loadDevice(w http.ResponseWriter, r *http.Request) (*store.Device, bool) {
	d, err := s.st.UserDevice(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if notFound(w, err) {
		return nil, false
	}
	return d, true
}

func (s *Server) deviceHead(r *http.Request, d *store.Device, tab string) map[string]any {
	installed, _ := s.st.DeviceAppIDs(r.Context(), d.ID)
	return map[string]any{
		"Device": deviceView{Device: d, Online: s.eng.Online(d.ID), AppCount: len(installed),
			NeedsUpdate: d.FWVersion != "" && d.FWVersion != fw.Version()},
		"Tab":       tab,
		"FWVersion": fw.Version(),
		"Saved":     r.URL.Query().Get("saved"),
		"Paired":    r.URL.Query().Get("paired") != "",
	}
}

type screenCard struct {
	S     render.Screen
	First bool
	Last  bool
}

type installedCard struct {
	engine.AppView
	Preview     *render.Screen // first screen, for the thumbnail
	First, Last bool
	NeedsFW     string // firmware version the board needs before it shows this app
}

// appChoice is an app that can be installed on a device.
type appChoice struct {
	ID, Name, Desc string
	By             string // publisher, for apps from the public list; "" for the user's own
	Installs       int
	ScreenCount    int
}

func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	installed := s.eng.DeviceApps(r.Context(), d, true)
	have := map[string]bool{}
	cards := make([]installedCard, len(installed))
	for i, a := range installed {
		have[a.ID] = true
		cards[i] = installedCard{AppView: a, First: i == 0, Last: i == len(installed)-1}
		if len(a.Screens) > 0 {
			cards[i].Preview = &a.Screens[0]
		}
		if c, ok := engine.CatalogEntry(a.ID); ok && !c.RunsOn(d.FWVersion) {
			cards[i].NeedsFW = c.NeedsFirmware()
		}
	}
	// Apps not on this device yet: the user's own, then the public list (most installed first).
	var choices []appChoice
	own := map[string]bool{}
	apps, _ := s.st.UserApps(r.Context(), d.UserID)
	for _, a := range apps {
		own[a.ID] = true
		if !have[a.ID] {
			screens, _ := s.st.AppScreens(r.Context(), a.ID)
			choices = append(choices, appChoice{ID: a.ID, Name: a.Name, ScreenCount: len(screens)})
		}
	}
	for _, l := range s.listings(r, nil) {
		if !have[l.ID] && !own[l.ID] {
			choices = append(choices, appChoice{ID: l.ID, Name: l.Name, Desc: l.Desc, By: l.By, Installs: l.Installs})
		}
	}
	data := s.deviceHead(r, d, "apps")
	size := s.eng.StateSize(r.Context(), d.ID)
	data["DataKB"] = fmt.Sprintf("%.1f", float64(size)/1024)
	data["DataMaxKB"] = engine.MaxStateBytes / 1024
	switch { // the board drops updates over MaxStateBytes; warn from 80%
	case size > engine.MaxStateBytes:
		data["DataLevel"] = "full"
	case size*10 > engine.MaxStateBytes*8:
		data["DataLevel"] = "near"
	default:
		data["DataLevel"] = ""
	}
	data["Installed"] = cards
	data["Choices"] = choices
	s.render(w, r, "device", data)
}

func (s *Server) deviceSettingsPage(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	s.render(w, r, "device_settings", s.deviceHead(r, d, "settings"))
}

// moveDeviceApp shifts an installed app one place in the board's drawer.
func (s *Server) moveDeviceApp(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	if err := s.st.MoveDeviceApp(r.Context(), d.ID, r.PathValue("app"), delta); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.eng.PushDevice(r.Context(), d.ID)
	http.Redirect(w, r, "/devices/"+d.ID, http.StatusSeeOther)
}
