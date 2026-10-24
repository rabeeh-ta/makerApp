package web

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"makerapp/internal/library"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

type deviceCheck struct {
	*store.Device
	Checked bool
	Online  bool
}

// screenEdit is the screen editor's data; with no id, for a new screen
// (?app= picks its app, ?source= its data source).
func (s *Server) screenEdit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sc := &store.Screen{Layout: "big_number"}
	if id := r.PathValue("id"); id != "" {
		var err error
		sc, err = s.st.UserScreen(r.Context(), u.ID, id)
		if notFound(w, err) {
			return
		}
	} else {
		sc.SourceID = r.URL.Query().Get("source")
		sc.AppID = r.URL.Query().Get("app")
		if sc.AppID == "" {
			// Coming from a data source: default to the user's first app ("" offers a new one).
			if apps, _ := s.st.UserApps(r.Context(), u.ID); len(apps) > 0 {
				sc.AppID = apps[0].ID
			}
		}
	}
	spec := render.ParseSpec(sc.Layout, sc.Spec)
	if sc.ID == "" {
		spec.Title = "My screen"
		spec.Value = "42"
		if sc.SourceID != "" {
			// Coming from "Make a screen": name it after the source and let the user pick fields.
			if src, err := s.st.UserSource(r.Context(), currentUser(r).ID, sc.SourceID); err == nil {
				spec.Title = src.Name
				spec.Value = ""
				sc.Name = src.Name
			}
		}
	}
	s.writeScreenEdit(w, r, sc, spec)
}

func (s *Server) writeScreenEdit(w http.ResponseWriter, r *http.Request, sc *store.Screen, spec render.Spec) {
	u := currentUser(r)
	var app *store.App
	if sc.AppID != "" {
		app, _ = s.st.UserApp(r.Context(), u.ID, sc.AppID)
	}
	apps, _ := s.st.UserApps(r.Context(), u.ID)
	sources, _ := s.st.UserSources(r.Context(), u.ID)
	devices, _ := s.st.UserDevices(r.Context(), u.ID)
	var showing []deviceCheck
	if sc.ID != "" {
		on := s.eng.DevicesShowing(r.Context(), sc.ID)
		for _, d := range devices {
			if slices.Contains(on, d.ID) {
				showing = append(showing, deviceCheck{Device: d, Online: s.eng.Online(d.ID), Checked: true})
			}
		}
	}
	var src *store.Source
	if sc.SourceID != "" {
		src, _ = s.st.UserSource(r.Context(), u.ID, sc.SourceID)
	}
	tmp := *sc
	b, _ := json.Marshal(spec)
	tmp.Spec = string(b)
	writeJSON(w, http.StatusOK, map[string]any{
		"Showing":  showing,
		"App":      app,
		"Apps":     apps,
		"Screen":   sc,
		"Spec":     spec,
		"Sources":  sources,
		"Preview":  toWeb(s.eng.PreviewScreen(r.Context(), &tmp, nil)),
		"Explorer": s.explorerData(src),
		// Values the board fills in itself, offered next to the source's fields.
		"DeviceFields": render.DeviceFields,
	})
}

func specFromForm(r *http.Request) (string, render.Spec) {
	layout := r.FormValue("layout")
	if !slices.Contains(render.Layouts, layout) {
		layout = "big_number"
	}
	spec := render.Spec{
		Title:      r.FormValue("title"),
		Accent:     r.FormValue("accent"),
		BG:         r.FormValue("bg"),
		Value:      r.FormValue("value"),
		Sub:        r.FormValue("sub"),
		SubColor:   r.FormValue("sub_color"),
		Body:       r.FormValue("body"),
		ItemsPath:  strings.TrimSpace(r.FormValue("items_path")),
		ItemLabel:  r.FormValue("item_label"),
		ItemValue:  r.FormValue("item_value"),
		MaxItems:   clamp(atoi(r.FormValue("max_items"), 10), 1, 30),
		SeriesPath: strings.TrimSpace(r.FormValue("series_path")),
		PctValue:   r.FormValue("pct_value"),
		PctMax:     r.FormValue("pct_max"),
	}
	if spec.BG == "#000000" {
		spec.BG = ""
	}
	return layout, spec
}

func (s *Server) saveScreen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "bad form")
		return
	}
	layout, spec := specFromForm(r)
	sc, err := s.lib.SaveScreen(r.Context(), currentUser(r).ID, r.PathValue("id"), library.ScreenInput{
		App: r.FormValue("app"), Name: r.FormValue("name"), Layout: layout, Spec: spec, SourceID: r.FormValue("source_id"),
	})
	if failed(w, err) {
		return
	}
	redirect(w, "/screens/"+sc.ID+"?saved=1")
}

func (s *Server) previewScreen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "bad form")
		return
	}
	layout, spec := specFromForm(r)
	sc, err := s.lib.Preview(r.Context(), currentUser(r).ID, library.ScreenInput{Layout: layout, Spec: spec, SourceID: r.FormValue("source_id")})
	if failed(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toWeb(sc))
}

type explorerData struct {
	Source *store.Source
	Paths  []render.Path
}

func (s *Server) explorerData(src *store.Source) explorerData {
	if src == nil {
		return explorerData{}
	}
	return explorerData{Source: src, Paths: render.Paths(src.LastData, 300)}
}

func (s *Server) explorer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var src *store.Source
	if id := r.URL.Query().Get("source_id"); id != "" {
		src, _ = s.st.UserSource(r.Context(), u.ID, id)
	}
	writeJSON(w, http.StatusOK, s.explorerData(src))
}

func (s *Server) deleteScreen(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := r.PathValue("id")
	back := "/apps"
	if sc, err := s.st.UserScreen(r.Context(), u.ID, id); err == nil && sc.AppID != "" {
		back = "/apps/" + sc.AppID
	}
	devices := s.eng.DevicesShowing(r.Context(), id)
	if err := s.st.DeleteScreen(r.Context(), u.ID, id); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	for _, d := range devices {
		s.eng.PushDevice(r.Context(), d)
	}
	redirect(w, back)
}
