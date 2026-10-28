package web

import (
	"net/http"
	"strconv"
	"strings"

	"makerapp/internal/engine"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

type deviceView struct {
	*store.Device
	Online      bool
	AppCount    int
	NeedsUpdate bool
	Preview     *webScreen // first installed app's first screen, for the card thumbnail
}

func (s *Server) deviceViews(r *http.Request) ([]deviceView, error) {
	u := currentUser(r)
	devices, err := s.st.UserDevices(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	out := make([]deviceView, 0, len(devices))
	for _, d := range devices {
		apps := s.eng.DeviceApps(r.Context(), d, true)
		v := deviceView{Device: d, Online: s.eng.Online(d.ID), AppCount: len(apps),
			NeedsUpdate: engine.FirmwareOutdated(d.FWVersion)}
		if len(apps) > 0 && len(apps[0].Screens) > 0 {
			v.Preview = toWeb(apps[0].Screens[0])
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	views, err := s.deviceViews(r)
	if notFound(w, err) {
		return
	}
	u := currentUser(r)
	sources, _ := s.st.UserSources(r.Context(), u.ID)
	screens, _ := s.st.UserScreens(r.Context(), u.ID)
	installed := false
	for _, v := range views {
		if v.AppCount > 0 {
			installed = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Devices": views,
		// Getting-started checklist, hidden once everything is done.
		"HasDevice": len(views) > 0, "HasSource": len(sources) > 0, "HasScreen": len(screens) > 0,
		"Installed": installed,
		"FirstDevice": func() string {
			if len(views) > 0 {
				return views[0].ID
			}
			return ""
		}(),
	})
}

func (s *Server) deviceStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d, err := s.st.UserDevice(r.Context(), u.ID, r.PathValue("id"))
	if notFound(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Online": s.eng.Online(d.ID), "LastSeen": d.LastSeen})
}

func (s *Server) saveDevice(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d, err := s.st.UserDevice(r.Context(), u.ID, r.PathValue("id"))
	if notFound(w, err) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = d.Name
	}
	settings := store.DeviceSettings{
		Name:       name,
		Brightness: clamp(atoi(r.FormValue("brightness"), d.Brightness), 5, 100),
		Flipped:    r.FormValue("flipped") == "1",
		OffsetX:    clamp(atoi(r.FormValue("offset_x"), 0), -20, 20),
		OffsetY:    clamp(atoi(r.FormValue("offset_y"), 0), -20, 20),
	}
	if err := s.st.UpdateDeviceSettings(r.Context(), u.ID, d.ID, settings); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	s.eng.PushDevice(r.Context(), d.ID)
	redirect(w, "/devices/"+d.ID+"/settings?saved=1")
}

func (s *Server) deviceAction(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	d, err := s.st.UserDevice(r.Context(), u.ID, r.PathValue("id"))
	if notFound(w, err) {
		return
	}
	msg := ""
	online := s.eng.Online(d.ID)
	switch r.PathValue("action") {
	case "identify":
		s.eng.Send(d.ID, map[string]any{"type": "identify"})
		msg = "Blinking."
	case "toast":
		text := strings.TrimSpace(r.FormValue("text"))
		if text == "" {
			text = "Hello from makerApp"
		}
		s.eng.Send(d.ID, map[string]any{"type": "toast", "text": render.ASCII(text), "ms": 4000})
		msg = "Message sent."
	case "sync":
		s.eng.PushDevice(r.Context(), d.ID)
		msg = "Apps re-sent."
	case "update":
		s.eng.OTA(d.ID)
		msg = "Updating. The board restarts in about a minute."
	case "reboot":
		s.eng.Send(d.ID, map[string]any{"type": "reboot"})
		msg = "Restarting."
	case "forget":
		if err := s.st.ForgetDevice(r.Context(), u.ID, d.ID); err != nil {
			fail(w, http.StatusInternalServerError, "server error")
			return
		}
		s.eng.Send(d.ID, map[string]any{"type": "unpaired"})
		redirect(w, "/devices")
		return
	default:
		fail(w, http.StatusNotFound, "no such action")
		return
	}
	if !online {
		msg = "The device is offline."
	}
	writeJSON(w, http.StatusOK, map[string]any{"Msg": msg, "Error": !online})
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func clamp(n, lo, hi int) int { return max(lo, min(hi, n)) }
