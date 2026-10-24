package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

type sourceView struct {
	*store.Source
	ScreenCount int
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	list, err := s.st.UserSources(r.Context(), u.ID)
	if notFound(w, err) {
		return
	}
	views := make([]sourceView, 0, len(list))
	for _, x := range list {
		ids, _ := s.st.ScreenIDsForSource(r.Context(), x.ID)
		views = append(views, sourceView{Source: x, ScreenCount: len(ids)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"Sources": views})
}

// sourceEdit is the source editor's data; with no id, for a new source.
func (s *Server) sourceEdit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	src := &store.Source{Kind: "http", Method: "GET", IntervalSec: 300}
	if id := r.PathValue("id"); id != "" {
		var err error
		src, err = s.st.UserSource(r.Context(), u.ID, id)
		if notFound(w, err) {
			return
		}
	}
	s.writeSourceEdit(w, r, src)
}

type kv struct{ K, V string }

func (s *Server) writeSourceEdit(w http.ResponseWriter, r *http.Request, src *store.Source) {
	sec := s.eng.OpenSecret(src)
	var headers []kv
	for _, line := range strings.Split(sec.Headers, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
			headers = append(headers, kv{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	var secretKeys []string
	for _, p := range sec.Params {
		secretKeys = append(secretKeys, p.K)
	}
	staticData := ""
	if src.Kind == "static" {
		staticData = src.LastData
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Source":     src,
		"HasAuth":    sec.Auth != "",
		"HeaderRows": headers,
		"SecretKeys": secretKeys,
		"StaticData": staticData,
		"Presets":    library.Presets,
		"PushURL":    s.publicBase(r) + "/push/" + src.PushToken,
		"Data":       s.sourceData(src),
		"Intervals": []struct {
			Sec   int
			Label string
		}{{15, "15 seconds"}, {30, "30 seconds"}, {60, "1 minute"}, {300, "5 minutes"}, {900, "15 minutes"},
			{1800, "30 minutes"}, {3600, "1 hour"}, {21600, "6 hours"}, {86400, "1 day"}},
	})
}

type sourceData struct {
	Source *store.Source
	Pretty string
	Paths  []render.Path
}

func (s *Server) sourceData(src *store.Source) sourceData {
	d := sourceData{Source: src}
	if src.LastData == "" {
		return d
	}
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(src.LastData), "", "  ") == nil {
		d.Pretty = buf.String()
	} else {
		d.Pretty = src.LastData
	}
	if len(d.Pretty) > 30000 {
		d.Pretty = d.Pretty[:30000] + "\n... (truncated)"
	}
	d.Paths = render.Paths(src.LastData, 300)
	return d
}

func (s *Server) saveSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "bad form")
		return
	}
	in := library.SourceInput{
		Name:        r.FormValue("name"),
		Kind:        r.FormValue("kind"),
		Method:      r.FormValue("method"),
		URL:         r.FormValue("url"),
		Body:        r.FormValue("body"),
		IntervalSec: atoi(r.FormValue("interval_sec"), 300),
		StaticData:  r.FormValue("static_data"),
	}
	sec := &library.SourceSecrets{Auth: r.FormValue("auth"), ClearAuth: r.FormValue("clear_auth") == "1"}
	hk, hv := r.Form["hk"], r.Form["hv"]
	for i := range hk {
		if i < len(hv) {
			sec.Headers = append(sec.Headers, fetch.Param{K: hk[i], V: hv[i]})
		}
	}
	sk, sv := r.Form["sk"], r.Form["sv"]
	for i := range sk {
		v := ""
		if i < len(sv) {
			v = sv[i]
		}
		sec.Params = append(sec.Params, fetch.Param{K: sk[i], V: v})
	}
	src, err := s.lib.SaveSource(r.Context(), currentUser(r).ID, r.PathValue("id"), in, sec)
	if failed(w, err) {
		return
	}
	redirect(w, "/sources/"+src.ID)
}

func (s *Server) fetchSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.lib.FetchSource(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if failed(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, s.sourceData(src))
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := r.PathValue("id")
	screens, _ := s.st.ScreenIDsForSource(r.Context(), id)
	if err := s.st.DeleteSource(r.Context(), u.ID, id); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	s.eng.PushScreens(r.Context(), screens...)
	redirect(w, "/sources")
}

// pushData lets anything POST JSON into a "push" source: curl, Zapier, a cron job.
func (s *Server) pushData(w http.ResponseWriter, r *http.Request) {
	src, err := s.st.SourceByPushToken(r.Context(), r.PathValue("token"))
	if err != nil {
		http.Error(w, `{"error":"unknown push token"}`, http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, `{"error":"read failed"}`, http.StatusBadRequest)
		return
	}
	if err := s.eng.SetPushedData(r.Context(), src, fetch.ToJSON(raw)); err != nil {
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
