package web

import (
	"fmt"
	"net/http"

	"makerapp/internal/engine"
	"makerapp/internal/fw"
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

type screenCard struct {
	S     *webScreen
	First bool
	Last  bool
}

type installedCard struct {
	engine.AppView
	Catalog     bool       // someone else's app: open it from the public list, not the editor
	By          string     // its publisher
	Preview     *webScreen // first screen, for the thumbnail
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

// device is everything the device's Apps and Settings tabs show.
func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	installed := s.eng.DeviceApps(r.Context(), d, true)
	have := map[string]bool{}
	cards := make([]installedCard, len(installed))
	for i, a := range installed {
		have[a.ID] = true
		cards[i] = installedCard{AppView: a, Catalog: a.Catalog, By: a.By, First: i == 0, Last: i == len(installed)-1}
		if len(a.Screens) > 0 {
			cards[i].Preview = toWeb(a.Screens[0])
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
	data := map[string]any{
		"Device": deviceView{Device: d, Online: s.eng.Online(d.ID), AppCount: len(installed),
			NeedsUpdate: d.FWVersion != "" && d.FWVersion != fw.Version()},
		"FWVersion": fw.Version(),
	}
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
	writeJSON(w, http.StatusOK, data)
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
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	s.eng.PushDevice(r.Context(), d.ID)
	redirect(w, "/devices/"+d.ID)
}
