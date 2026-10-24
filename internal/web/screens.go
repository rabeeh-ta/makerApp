package web

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

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
	u := currentUser(r)
	sc := &store.Screen{UserID: u.ID}
	if id := r.PathValue("id"); id != "" {
		var err error
		sc, err = s.st.UserScreen(r.Context(), u.ID, id)
		if notFound(w, err) {
			return
		}
	}
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "bad form")
		return
	}
	layout, spec := specFromForm(r)
	sc.Layout = layout
	sc.Name = strings.TrimSpace(r.FormValue("name"))
	sc.SourceID = r.FormValue("source_id")
	if sc.SourceID != "" {
		if _, err := s.st.UserSource(r.Context(), u.ID, sc.SourceID); err != nil {
			sc.SourceID = ""
		}
	}
	if sc.Name == "" {
		sc.Name = spec.Title
	}
	if sc.Name == "" {
		sc.Name = "Untitled screen"
	}
	if sc.ID == "" {
		if err := s.screenApp(r, sc); err != nil {
			fail(w, http.StatusBadRequest, "Could not save: "+err.Error())
			return
		}
	}
	b, _ := json.Marshal(spec)
	sc.Spec = string(b)
	if err := s.st.SaveScreen(r.Context(), sc); err != nil {
		fail(w, http.StatusBadRequest, "Could not save: "+err.Error())
		return
	}

	// Every device with this screen's app installed updates now.
	s.eng.PushScreens(r.Context(), sc.ID)
	redirect(w, "/screens/"+sc.ID+"?saved=1")
}

// screenApp puts a new screen in the app picked on the form. Picking "new"
// makes an app named after the screen, installed on the user's board when
// they have exactly one.
func (s *Server) screenApp(r *http.Request, sc *store.Screen) error {
	u := currentUser(r)
	if a, err := s.st.UserApp(r.Context(), u.ID, r.FormValue("app")); err == nil {
		sc.AppID = a.ID
		return nil
	}
	a := &store.App{UserID: u.ID, Name: sc.Name}
	if err := s.st.SaveApp(r.Context(), a); err != nil {
		return err
	}
	sc.AppID = a.ID
	if devices, _ := s.st.UserDevices(r.Context(), u.ID); len(devices) == 1 {
		return s.st.InstallApp(r.Context(), devices[0].ID, a.ID)
	}
	return nil
}

func (s *Server) previewScreen(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "bad form")
		return
	}
	layout, spec := specFromForm(r)
	b, _ := json.Marshal(spec)
	sc := &store.Screen{ID: "preview", Layout: layout, Spec: string(b)}
	if id := r.FormValue("source_id"); id != "" {
		if _, err := s.st.UserSource(r.Context(), u.ID, id); err == nil {
			sc.SourceID = id
		}
	}
	writeJSON(w, http.StatusOK, toWeb(s.eng.PreviewScreen(r.Context(), sc, nil)))
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
