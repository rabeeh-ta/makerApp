package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"makerapp/internal/fetch"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

// Preset prefills the request form. Presets are only shortcuts; every source is
// just a request (with an optional token) whose JSON you map onto screens.
type PresetParam struct {
	K      string `json:"k"`
	V      string `json:"v"`
	Secret bool   `json:"secret,omitempty"`
}

type Preset struct {
	Name     string        `json:"name"`
	Desc     string        `json:"desc"`
	Badge    string        `json:"badge"` // 2-letter mark on the template card
	Method   string        `json:"method"`
	URL      string        `json:"url"`
	Params   []PresetParam `json:"params,omitempty"`
	Body     string        `json:"body,omitempty"`
	Headers  []PresetParam `json:"headers,omitempty"`
	AuthHint string        `json:"auth_hint,omitempty"`
	Interval int           `json:"interval,omitempty"`
}

var presets = []Preset{
	{Name: "Blank request", Desc: "Any URL that returns JSON", Badge: "{}", Method: "GET"},
	{Name: "Meteosource weather", Desc: "Current weather; free key from meteosource.com", Badge: "MS", Method: "GET",
		URL:    "https://www.meteosource.com/api/v1/free/point?lat=11.2588&lon=75.7804&sections=current&timezone=auto&language=en&units=metric",
		Params: []PresetParam{{K: "key", Secret: true}}, Interval: 900},
	{Name: "Open-Meteo weather", Desc: "Temperature + hourly forecast, no key", Badge: "OM", Method: "GET",
		URL: "https://api.open-meteo.com/v1/forecast?latitude=12.97&longitude=77.59&current=temperature_2m,wind_speed_10m&hourly=temperature_2m&forecast_days=1", Interval: 900},
	{Name: "PostHog SQL", Desc: "Run a HogQL query on your project", Badge: "PH", Method: "POST",
		URL:      "https://us.posthog.com/api/projects/PROJECT_ID/query/",
		Body:     `{"query": {"kind": "HogQLQuery", "query": "SELECT count() FROM events WHERE timestamp > now() - INTERVAL 1 DAY"}}`,
		AuthHint: "Personal API key (phx_...)", Interval: 300},
	{Name: "PostHog insight", Desc: "Results of a saved insight", Badge: "PH", Method: "GET",
		URL: "https://us.posthog.com/api/projects/PROJECT_ID/insights/INSIGHT_ID/?refresh=blocking", AuthHint: "Personal API key (phx_...)", Interval: 300},
	{Name: "HubSpot top deals", Desc: "Biggest deals by amount", Badge: "HS", Method: "POST",
		URL:      "https://api.hubapi.com/crm/v3/objects/deals/search",
		Body:     `{"limit": 10, "properties": ["dealname", "amount", "dealstage"], "sorts": [{"propertyName": "amount", "direction": "DESCENDING"}]}`,
		AuthHint: "Private app token (pat-...)", Interval: 300},
	{Name: "HubSpot contact count", Desc: "Total contacts in your CRM", Badge: "HS", Method: "POST",
		URL: "https://api.hubapi.com/crm/v3/objects/contacts/search", Body: `{"limit": 1}`, AuthHint: "Private app token (pat-...)", Interval: 900},
	{Name: "Crypto prices", Desc: "CoinGecko prices + 24h change, no key", Badge: "CG", Method: "GET",
		URL: "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd&include_24hr_change=true", Interval: 300},
	{Name: "GitHub repo", Desc: "Stars, forks, open issues", Badge: "GH", Method: "GET", URL: "https://api.github.com/repos/PostHog/posthog", Interval: 1800},
}

type sourceView struct {
	*store.Source
	ScreenCount int
}

func (s *Server) sourcesPage(w http.ResponseWriter, r *http.Request) {
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
	s.render(w, r, "sources", map[string]any{"Sources": views})
}

func (s *Server) sourceEditPage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	src := &store.Source{Kind: "http", Method: "GET", IntervalSec: 300}
	if id := r.PathValue("id"); id != "" {
		var err error
		src, err = s.st.UserSource(r.Context(), u.ID, id)
		if notFound(w, err) {
			return
		}
	}
	s.renderSourceEdit(w, r, src, "")
}

type kv struct{ K, V string }

func (s *Server) renderSourceEdit(w http.ResponseWriter, r *http.Request, src *store.Source, errMsg string) {
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
	s.render(w, r, "source_edit", map[string]any{
		"Source":     src,
		"HasAuth":    sec.Auth != "",
		"HeaderRows": headers,
		"SecretKeys": secretKeys,
		"StaticData": staticData,
		"Presets":    presets,
		"Error":      errMsg,
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
	u := currentUser(r)
	src := &store.Source{UserID: u.ID}
	if id := r.PathValue("id"); id != "" {
		var err error
		src, err = s.st.UserSource(r.Context(), u.ID, id)
		if notFound(w, err) {
			return
		}
	}
	sec := s.eng.OpenSecret(src)
	src.Name = strings.TrimSpace(r.FormValue("name"))
	src.Kind = r.FormValue("kind")
	src.Method = strings.ToUpper(r.FormValue("method"))
	src.URL = strings.TrimSpace(r.FormValue("url"))
	src.Body = strings.TrimSpace(r.FormValue("body"))
	src.IntervalSec = clamp(atoi(r.FormValue("interval_sec"), 300), 15, 86400)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Header rows -> "Name: value" lines (stored encrypted).
	var lines []string
	hk, hv := r.Form["hk"], r.Form["hv"]
	for i := range hk {
		if k := strings.TrimSpace(hk[i]); k != "" && i < len(hv) {
			lines = append(lines, k+": "+strings.TrimSpace(hv[i]))
		}
	}
	sec.Headers = strings.Join(lines, "\n")
	// Secret query params: kept out of the URL, added when fetching.
	old := map[string]string{}
	for _, p := range sec.Params {
		old[p.K] = p.V
	}
	sec.Params = nil
	sk, sv := r.Form["sk"], r.Form["sv"]
	for i := range sk {
		k := strings.TrimSpace(sk[i])
		if k == "" {
			continue
		}
		v := ""
		if i < len(sv) {
			v = strings.TrimSpace(sv[i])
		}
		if v == "" {
			v = old[k] // blank means "keep the saved value"
		}
		if v != "" {
			sec.Params = append(sec.Params, fetch.Param{K: k, V: v})
		}
	}
	src.URL = stripParams(src.URL, sec.Params)
	if a := strings.TrimSpace(r.FormValue("auth")); a != "" {
		sec.Auth = a
	}
	if r.FormValue("clear_auth") == "1" {
		sec.Auth = ""
	}
	src.Secret = s.eng.SealSecret(sec)

	if src.Name == "" {
		s.renderSourceEdit(w, r, src, "Give the source a name.")
		return
	}
	switch src.Kind {
	case "http":
		if !strings.HasPrefix(src.URL, "http://") && !strings.HasPrefix(src.URL, "https://") {
			s.renderSourceEdit(w, r, src, "The URL must start with https:// (or http://).")
			return
		}
		if src.Method != "GET" && src.Method != "POST" && src.Method != "PUT" && src.Method != "PATCH" {
			src.Method = "GET"
		}
		if src.Body != "" && src.Method != "GET" && !json.Valid([]byte(src.Body)) {
			s.renderSourceEdit(w, r, src, "The request body isn't valid JSON.")
			return
		}
	case "static":
		src.LastData = fetch.ToJSON([]byte(strings.TrimSpace(r.FormValue("static_data"))))
		src.LastFetchedAt = time.Now().Unix()
		src.LastError = ""
	case "push":
		if src.PushToken == "" {
			src.PushToken = store.RandomToken(16)
		}
	default:
		s.renderSourceEdit(w, r, src, "Pick a source type.")
		return
	}
	if err := s.st.SaveSource(r.Context(), src); err != nil {
		s.renderSourceEdit(w, r, src, "Could not save: "+err.Error())
		return
	}
	if src.Kind == "http" {
		// Fetch right away so the response is ready to map onto screens.
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		_ = s.eng.Refresh(ctx, src)
		cancel()
	} else {
		s.eng.PushSource(r.Context(), src.ID)
	}
	http.Redirect(w, r, "/sources/"+src.ID, http.StatusSeeOther)
}

// stripParams removes secret keys from a URL in case they were also typed there.
func stripParams(raw string, params []fetch.Param) string {
	if len(params) == 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	drop := map[string]bool{}
	for _, p := range params {
		drop[p.K] = true
	}
	// Edit the raw query by hand so the remaining params keep their order and encoding.
	var keep []string
	for _, part := range strings.Split(u.RawQuery, "&") {
		k, _, _ := strings.Cut(part, "=")
		if dk, err := url.QueryUnescape(k); part != "" && (err != nil || !drop[dk]) {
			keep = append(keep, part)
		}
	}
	u.RawQuery = strings.Join(keep, "&")
	return u.String()
}

func (s *Server) fetchSource(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	src, err := s.st.UserSource(r.Context(), u.ID, r.PathValue("id"))
	if notFound(w, err) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	_ = s.eng.Refresh(ctx, src)
	cancel()
	src, _ = s.st.UserSource(r.Context(), u.ID, src.ID)
	s.partial(w, "source_data", s.sourceData(src))
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := r.PathValue("id")
	screens, _ := s.st.ScreenIDsForSource(r.Context(), id)
	if err := s.st.DeleteSource(r.Context(), u.ID, id); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.eng.PushScreens(r.Context(), screens...)
	http.Redirect(w, r, "/sources", http.StatusSeeOther)
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
