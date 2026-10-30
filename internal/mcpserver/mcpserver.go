// Package mcpserver is makerApp's MCP connector: the tools an AI client
// (Claude, Codex, …) uses to build apps for the user who signed it in. The
// endpoint and the sign-in live in internal/web; this package decides what the
// AI can see and do.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/limit"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

// Deps are what the tools work with.
type Deps struct {
	Store     *store.Store
	Engine    *engine.Engine
	Library   *library.Library
	Fetch     *fetch.Client
	PublicURL string // for links back to makerApp
	Log       *slog.Logger
}

type server struct {
	Deps
	lim *limit.Limiter
}

// Handler is the MCP endpoint: Streamable HTTP, stateless. internal/web puts
// the sign-in check in front of it.
func Handler(d Deps) http.Handler {
	s := &server{Deps: d, lim: limit.New()}
	srv := mcp.NewServer(&mcp.Implementation{Name: "makerapp", Title: "makerApp", Version: "1.0.0", WebsiteURL: d.PublicURL},
		&mcp.ServerOptions{Instructions: Instructions(), Logger: d.Log})
	s.register(srv)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true, // each request stands alone, so a deploy doesn't break connected clients
		JSONResponse: true,
		// Sideyard's proxy reaches the app on 127.0.0.1 with the public Host header,
		// which this check refuses. Every request carries a bearer token, so DNS
		// rebinding gains an attacker nothing.
		DisableLocalhostProtection: true,
		Logger:                     d.Log,
	})
}

// Instructions is what an AI client reads when it connects: what makerApp is,
// how to build an app, and how the board's screen works.
func Instructions() string {
	var fields []string
	for _, f := range render.DeviceFields {
		fields = append(fields, "device."+f.Name)
	}
	return `makerApp puts data on a small desk display, the makerDevice board (240x135 screen, three buttons). You're connected to the user's makerApp account.

What things are:
- A data source fetches JSON: a request (kind http, refreshed on an interval), fixed data (static), or a webhook (push) that scripts POST JSON to. A push source can be personal (per_user): when the app is published, everyone who installs it gets their own push link and sees only their own data. Use it for data about the person (their usage, their steps), not shared data (a price, the weather).
- An app is a named set of screens. The board lists installed apps in its Apps drawer. Inside an app the top and bottom buttons move between screens and the middle button goes back; cycle_sec turns the screens by itself.
- A screen shows data in a layout. Its text fields are templates, {{ path | formatter }}, reading the source's JSON with gjson paths.

How to build an app:
1. Call get_overview to see the user's boards, apps and data sources.
2. Find the data: reuse a source, start from list_templates, or explore an API with try_request first. Then create_source; its answer has a sample of the data and its paths.
3. create_app, then add_screen for each screen. Every screen answer shows what the board would display and lists checks: fix each one (shorten text, correct paths) with update_screen before you go on. preview_screen tries a screen without saving.
4. When the user wants it on their board, install_app (or they click Install in makerApp). Changes to an installed app reach the board within a second.
Answers include makerApp links; share them so the user can see what you built.

API keys: never ask the user to paste a key, token or password into the chat. If an API needs a key (or answers 401/403), create the source without it, give the user the source's link to add the key in makerApp, then call fetch_source.
You can't delete anything, publish apps, or install apps other people published: the user does those in makerApp.

` + render.LayoutGuide() + `

Templates:
- {{ path }} reads a value: results.0.count, data.items.#.price (every price), name\.with\.dots.
- Formatters, chained with |: num (1,234), compact (1.2k, 3.4M), money ($83.5k), pct (adds %), pct100 (x100, then %), fixed:N (N decimals), signed (+3), sum, avg, min, max, len, first, last (on lists), upper, lower, ago (5m ago), date (Oct 1), time (14:05).
- {{ @ago }} is when the source was last fetched; {{ @time }} and {{ @date }} are now.
- ` + strings.Join(fields, ", ") + ` are filled in by the board itself and stay live offline: use them for clocks and status screens without a source.
- big_number example: title "Signups today", value "{{ results.0.0 | num }}", sub "{{ change | signed }}% vs yesterday", sub_color "auto".
- list example: items_path "results", item_label "{{ properties.dealname }}", item_value "{{ properties.amount | money }}".

Good screens: one idea per screen, a short title, a value that fits the biggest font, a subtitle that gives context (since when, compared to what). Most APIs are fine refreshed every 5 to 15 minutes; 15 seconds is the fastest.`
}

func ptr[T any](v T) *T { return &v }

func (s *server) register(srv *mcp.Server) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	readWeb := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
	write := &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
	writeWeb := &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)}
	board := &mcp.ToolAnnotations{DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)}

	mcp.AddTool(srv, &mcp.Tool{Name: "get_overview", Title: "Overview", Annotations: read,
		Description: "The user's boards (online, firmware, installed apps, how full), apps, data sources and makerApp's built-in apps. Start here."}, s.overview)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_guide", Title: "Screen guide", Annotations: read,
		Description: "How makerApp and the board's screen work: layouts, how much text fits, templates and formatters. The same text as the server instructions."}, s.guide)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_templates", Title: "Data source templates", Annotations: read,
		Description: "Ready-made data source settings (weather, crypto prices, GitHub, PostHog, HubSpot) to start from."}, s.templates)
	mcp.AddTool(srv, &mcp.Tool{Name: "try_request", Title: "Try a request", Annotations: readWeb,
		Description: "Fetch a URL without saving anything, to explore an API. Returns the status, a sample of the JSON and its paths."}, s.tryRequest)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_source", Title: "Get a data source", Annotations: read,
		Description: "A data source's settings, latest status, a sample of its data and the paths templates can use."}, s.getSource)
	mcp.AddTool(srv, &mcp.Tool{Name: "create_source", Title: "Create a data source", Annotations: writeWeb,
		Description: "Create a data source. A request (kind http) is fetched right away; the answer has the data's sample and paths. Never put API keys here: the user adds them in makerApp."}, s.createSource)
	mcp.AddTool(srv, &mcp.Tool{Name: "update_source", Title: "Change a data source", Annotations: writeWeb,
		Description: "Change a data source's settings; only the fields given change. Saved keys stay as they are."}, s.updateSource)
	mcp.AddTool(srv, &mcp.Tool{Name: "fetch_source", Title: "Fetch a data source now", Annotations: readWeb,
		Description: "Fetch a request now, for example after the user added a key, and return the result."}, s.fetchSource)
	mcp.AddTool(srv, &mcp.Tool{Name: "get_app", Title: "Get an app", Annotations: read,
		Description: "An app with its screens: their fields, what the board shows now, and checks to fix."}, s.getApp)
	mcp.AddTool(srv, &mcp.Tool{Name: "create_app", Title: "Create an app", Annotations: write,
		Description: "Create an empty app; then add screens with add_screen."}, s.createApp)
	mcp.AddTool(srv, &mcp.Tool{Name: "update_app", Title: "Change an app", Annotations: write,
		Description: "Rename an app, change its description, or set how often it turns to the next screen by itself."}, s.updateApp)
	mcp.AddTool(srv, &mcp.Tool{Name: "preview_screen", Title: "Preview a screen", Annotations: read,
		Description: "Render a screen as the board would show it, with checks, without saving it."}, s.previewScreen)
	mcp.AddTool(srv, &mcp.Tool{Name: "add_screen", Title: "Add a screen", Annotations: write,
		Description: "Add a screen to the end of an app. The answer shows what the board displays and lists checks to fix."}, s.addScreen)
	mcp.AddTool(srv, &mcp.Tool{Name: "update_screen", Title: "Change a screen", Annotations: write,
		Description: "Change a screen; only the fields given change. Boards with its app update within a second."}, s.updateScreen)
	mcp.AddTool(srv, &mcp.Tool{Name: "reorder_screens", Title: "Reorder screens", Annotations: board,
		Description: "Put an app's screens in a new order; list every screen id once."}, s.reorderScreens)
	mcp.AddTool(srv, &mcp.Tool{Name: "install_app", Title: "Install an app on a board", Annotations: board,
		Description: "Put one of the user's apps, or a built-in app, on one of their boards. It joins the end of the board's Apps drawer."}, s.installApp)
	mcp.AddTool(srv, &mcp.Tool{Name: "uninstall_app", Title: "Uninstall an app from a board", Annotations: board,
		Description: "Take an app off a board. The app stays in makerApp."}, s.uninstallApp)
}

// ---- who's calling ----

type caller struct{ userID, client string }

func (s *server) caller(req *mcp.CallToolRequest) (caller, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return caller{}, errors.New("not signed in")
	}
	info := req.Extra.TokenInfo
	if !s.lim.Allow("mcp:"+info.UserID, 120, time.Minute) {
		return caller{}, errors.New("too many requests: makerApp allows 120 tool calls a minute; wait a moment")
	}
	c := caller{userID: info.UserID, client: "AI"}
	if name, _ := info.Extra["client"].(string); name != "" {
		c.client = name
	}
	return c, nil
}

// fail turns a library or store error into a message for the AI.
func fail(err error, what string) error {
	if e, ok := library.IsUserError(err); ok {
		return errors.New(e.Msg)
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no such %s in this account", what)
	}
	return err
}

func (s *server) link(path string) string { return strings.TrimRight(s.PublicURL, "/") + path }

// ---- views ----

type appRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type boardView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Online      bool     `json:"online"`
	Firmware    string   `json:"firmware"`
	Apps        []appRef `json:"installed_apps" jsonschema:"in Apps drawer order"`
	DataKB      float64  `json:"apps_data_kb" jsonschema:"app content sent to the board"`
	DataLimitKB int      `json:"apps_data_limit_kb" jsonschema:"the most the board takes; past it, it stops updating"`
	Warning     string   `json:"warning,omitempty"`
	Link        string   `json:"link"`
}

func (s *server) board(ctx context.Context, d *store.Device) boardView {
	v := boardView{ID: d.ID, Name: d.Name, Online: s.Engine.Online(d.ID), Firmware: d.FWVersion,
		DataLimitKB: engine.MaxStateBytes / 1024, Link: s.link("/devices/" + d.ID), Apps: []appRef{}}
	for _, a := range s.Engine.DeviceApps(ctx, d, true) {
		v.Apps = append(v.Apps, appRef{a.ID, a.Name})
	}
	size := s.Engine.StateSize(ctx, d.ID)
	v.DataKB = float64(size*10/1024) / 10
	switch {
	case size > engine.MaxStateBytes:
		v.Warning = "The apps' content is over the board's limit, so it can't update. Uninstall an app or show fewer list rows."
	case size*10 > engine.MaxStateBytes*8:
		v.Warning = "The board is nearly full; adding more may stop it from updating."
	}
	return v
}

type sourceView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Method      string   `json:"method,omitempty"`
	URL         string   `json:"url,omitempty"`
	Body        string   `json:"body,omitempty"`
	IntervalSec int      `json:"interval_sec,omitempty"`
	HasKey      bool     `json:"has_key" jsonschema:"whether a token, secret parameter or header is saved (never shown)"`
	Status      int      `json:"status,omitempty"`
	Error       string   `json:"error,omitempty"`
	Fetched     string   `json:"fetched"`
	Sample      string   `json:"sample,omitempty" jsonschema:"the latest data, trimmed"`
	Paths       []string `json:"paths,omitempty" jsonschema:"paths templates can use, with example values"`
	PushURL     string   `json:"push_url,omitempty" jsonschema:"webhook sources: POST JSON here"`
	PerUser     bool     `json:"per_user,omitempty" jsonschema:"personal: everyone who installs the published app sends their own data"`
	Example     string   `json:"example,omitempty" jsonschema:"personal: the example data App Store previews show"`
	NeedsKey    string   `json:"needs_key,omitempty"`
	Link        string   `json:"link"`
}

func (s *server) sourceView(src *store.Source) sourceView {
	sec := s.Engine.OpenSecret(src)
	v := sourceView{ID: src.ID, Name: src.Name, Kind: src.Kind, Status: src.LastStatus, Error: src.LastError,
		HasKey: sec.Auth != "" || sec.Headers != "" || len(sec.Params) > 0, Fetched: ago(src.LastFetchedAt),
		Link: s.link("/sources/" + src.ID)}
	switch src.Kind {
	case "http":
		v.Method, v.URL, v.Body, v.IntervalSec = src.Method, src.URL, src.Body, src.IntervalSec
	case "push":
		v.PushURL, v.PerUser, v.Example = s.link("/push/"+src.PushToken), src.PerUser, src.Example
	}
	v.Sample, v.Paths = sample(src.LastData)
	if src.LastStatus == http.StatusUnauthorized || src.LastStatus == http.StatusForbidden {
		v.NeedsKey = "This API needs a key. Ask the user to open " + v.Link + " and add it there (Token field, or a secret query parameter), then call fetch_source. Don't ask for the key in the chat."
	}
	return v
}

// sample is the data trimmed for a model to read, and its paths.
func sample(data string) (string, []string) {
	if data == "" {
		return "", nil
	}
	var b bytes.Buffer
	pretty := data
	if json.Indent(&b, []byte(data), "", " ") == nil {
		pretty = b.String()
	}
	if len(pretty) > 3000 {
		pretty = pretty[:3000] + "\n… (trimmed)"
	}
	var paths []string
	for _, p := range render.Paths(data, 150) {
		paths = append(paths, p.Path+" = "+p.Preview)
	}
	return pretty, paths
}

func ago(unix int64) string {
	if unix == 0 {
		return "never"
	}
	return time.Since(time.Unix(unix, 0)).Round(time.Second).String() + " ago"
}

type shown struct {
	Title        string     `json:"title,omitempty"`
	Value        string     `json:"value,omitempty"`
	Sub          string     `json:"sub,omitempty"`
	Body         string     `json:"body,omitempty"`
	Rows         [][]string `json:"rows,omitempty" jsonschema:"the first page of list rows: label, value"`
	TotalRows    int        `json:"total_rows,omitempty"`
	SeriesPoints int        `json:"series_points,omitempty"`
	ProgressPct  int        `json:"progress_pct,omitempty"`
}

type screenView struct {
	ID       string      `json:"id,omitempty"`
	Name     string      `json:"name"`
	Layout   string      `json:"layout"`
	SourceID string      `json:"source_id,omitempty"`
	Fields   render.Spec `json:"fields"`
	Shows    shown       `json:"shows" jsonschema:"what the board shows now"`
	Checks   []string    `json:"checks" jsonschema:"problems to fix; empty when the screen looks right"`
}

func (s *server) screenView(ctx context.Context, id, name, layout, sourceID string, spec render.Spec, out render.Screen, screens int) screenView {
	data := ""
	if sourceID != "" {
		if src, err := s.Store.Source(ctx, sourceID); err == nil {
			data = src.LastData
		}
	}
	v := screenView{ID: id, Name: name, Layout: layout, SourceID: sourceID, Fields: spec, Checks: render.Check(out, spec, data, screens),
		Shows: shown{Title: out.Title, Value: out.Value, Sub: out.Sub, Body: out.Body, TotalRows: len(out.Items),
			SeriesPoints: len(out.Series), ProgressPct: int(out.Pct * 100)}}
	for i, it := range out.Items {
		if i == 5 {
			break
		}
		v.Shows.Rows = append(v.Shows.Rows, []string{it[0], it[1]})
	}
	if v.Checks == nil {
		v.Checks = []string{}
	}
	return v
}

func (s *server) savedScreen(ctx context.Context, sc *store.Screen) screenView {
	screens, _ := s.Store.AppScreens(ctx, sc.AppID)
	return s.screenView(ctx, sc.ID, sc.Name, sc.Layout, sc.SourceID, render.ParseSpec(sc.Layout, sc.Spec),
		s.Engine.PreviewScreen(ctx, sc, nil), len(screens))
}

type appView struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	CycleSec    int          `json:"cycle_sec"`
	MadeBy      string       `json:"made_by,omitempty"`
	Published   bool         `json:"published"`
	Screens     []screenView `json:"screens"`
	InstalledOn []appRef     `json:"installed_on"`
	Link        string       `json:"link"`
}

func (s *server) appView(ctx context.Context, userID string, a *store.App) appView {
	v := appView{ID: a.ID, Name: a.Name, Description: a.Description, CycleSec: a.CycleSec, MadeBy: a.MadeBy,
		Published: a.PublishedAt > 0, Link: s.link("/apps/" + a.ID), Screens: []screenView{}, InstalledOn: []appRef{}}
	screens, _ := s.Store.AppScreens(ctx, a.ID)
	for _, sc := range screens {
		v.Screens = append(v.Screens, s.savedScreen(ctx, sc))
	}
	on, _ := s.Store.DevicesWithApp(ctx, a.ID)
	devices, _ := s.Store.UserDevices(ctx, userID)
	for _, d := range devices {
		if slices.Contains(on, d.ID) {
			v.InstalledOn = append(v.InstalledOn, appRef{d.ID, d.Name})
		}
	}
	return v
}

// ---- overview and reference ----

type appSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Screens     int      `json:"screens"`
	MadeBy      string   `json:"made_by,omitempty"`
	InstalledOn []string `json:"installed_on" jsonschema:"board ids"`
	Link        string   `json:"link"`
}

type sourceSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Status  int    `json:"status,omitempty"`
	Error   string `json:"error,omitempty"`
	Fetched string `json:"fetched"`
	Link    string `json:"link"`
}

type builtIn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"description"`
}

type overview struct {
	Boards  []boardView     `json:"boards"`
	Apps    []appSummary    `json:"apps"`
	Sources []sourceSummary `json:"sources"`
	BuiltIn []builtIn       `json:"built_in_apps" jsonschema:"makerApp's own apps (clock, games, tools); they install like the user's apps"`
	Next    string          `json:"next,omitempty"`
}

func (s *server) overview(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, overview, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, overview{}, err
	}
	out := overview{Boards: []boardView{}, Apps: []appSummary{}, Sources: []sourceSummary{}}
	devices, _ := s.Store.UserDevices(ctx, c.userID)
	for _, d := range devices {
		out.Boards = append(out.Boards, s.board(ctx, d))
	}
	apps, _ := s.Store.UserApps(ctx, c.userID)
	for _, a := range apps {
		screens, _ := s.Store.AppScreens(ctx, a.ID)
		on, _ := s.Store.DevicesWithApp(ctx, a.ID)
		if on == nil {
			on = []string{}
		}
		out.Apps = append(out.Apps, appSummary{ID: a.ID, Name: a.Name, Screens: len(screens), MadeBy: a.MadeBy, InstalledOn: on, Link: s.link("/apps/" + a.ID)})
	}
	sources, _ := s.Store.UserSources(ctx, c.userID)
	for _, src := range sources {
		out.Sources = append(out.Sources, sourceSummary{ID: src.ID, Name: src.Name, Kind: src.Kind, Status: src.LastStatus,
			Error: src.LastError, Fetched: ago(src.LastFetchedAt), Link: s.link("/sources/" + src.ID)})
	}
	for _, a := range engine.Catalog {
		out.BuiltIn = append(out.BuiltIn, builtIn{a.ID, a.Name, a.Desc})
	}
	if len(devices) == 0 {
		out.Next = "The user has no board yet. Apps can still be built; to show them, the user adds a board at " + s.link("/flash") + "."
	}
	return nil, out, nil
}

func (s *server) guide(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
	Guide string `json:"guide"`
}, error) {
	var out struct {
		Guide string `json:"guide"`
	}
	if _, err := s.caller(req); err != nil {
		return nil, out, err
	}
	out.Guide = Instructions()
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Guide}}}, out, nil
}

type templateView struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	Body        string   `json:"body,omitempty"`
	IntervalSec int      `json:"interval_sec,omitempty"`
	KeyNeeded   string   `json:"key_needed,omitempty" jsonschema:"the key the user adds in makerApp after you create the source"`
	SecretKeys  []string `json:"secret_params,omitempty" jsonschema:"query parameters the user fills in as secrets"`
}

func (s *server) templates(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
	Templates []templateView `json:"templates"`
}, error) {
	var out struct {
		Templates []templateView `json:"templates"`
	}
	if _, err := s.caller(req); err != nil {
		return nil, out, err
	}
	for _, p := range library.Presets {
		if p.URL == "" {
			continue // the blank request
		}
		t := templateView{Name: p.Name, Description: p.Desc, Method: p.Method, URL: p.URL, Body: p.Body, IntervalSec: p.Interval, KeyNeeded: p.AuthHint}
		for _, q := range p.Params {
			if q.Secret {
				t.SecretKeys = append(t.SecretKeys, q.K)
			}
		}
		out.Templates = append(out.Templates, t)
	}
	return nil, out, nil
}

// ---- data sources ----

type tryIn struct {
	Method string `json:"method,omitempty" jsonschema:"GET (default), POST, PUT or PATCH"`
	URL    string `json:"url" jsonschema:"https URL that returns JSON"`
	Body   string `json:"body,omitempty" jsonschema:"JSON body for POST, PUT and PATCH"`
}

type tryOut struct {
	Status int      `json:"status"`
	Sample string   `json:"sample,omitempty"`
	Paths  []string `json:"paths,omitempty"`
}

func (s *server) tryRequest(ctx context.Context, req *mcp.CallToolRequest, in tryIn) (*mcp.CallToolResult, tryOut, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, tryOut{}, err
	}
	if !s.lim.Allow("fetch:"+c.userID, 30, time.Minute) {
		return nil, tryOut{}, errors.New("too many requests: wait a minute before trying more URLs")
	}
	if !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
		return nil, tryOut{}, errors.New("the URL must start with https://")
	}
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = "GET"
	}
	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := s.Fetch.Do(fctx, fetch.Request{Method: method, URL: in.URL, Body: in.Body})
	if err != nil {
		return nil, tryOut{}, err
	}
	out := tryOut{Status: res.Status}
	out.Sample, out.Paths = sample(res.Data)
	return nil, out, nil
}

type sourceID struct {
	SourceID string `json:"source_id"`
}

func (s *server) getSource(ctx context.Context, req *mcp.CallToolRequest, in sourceID) (*mcp.CallToolResult, sourceView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, sourceView{}, err
	}
	src, err := s.Store.UserSource(ctx, c.userID, in.SourceID)
	if err != nil {
		return nil, sourceView{}, fail(err, "data source")
	}
	return nil, s.sourceView(src), nil
}

type createSourceIn struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty" jsonschema:"http (default): fetch a URL on an interval; static: fixed JSON; push: a webhook scripts POST JSON to"`
	Method      string `json:"method,omitempty" jsonschema:"http: GET (default), POST, PUT or PATCH"`
	URL         string `json:"url,omitempty" jsonschema:"http: the URL, query parameters included"`
	Body        string `json:"body,omitempty" jsonschema:"http: JSON body for POST, PUT and PATCH"`
	IntervalSec int    `json:"interval_sec,omitempty" jsonschema:"http: seconds between fetches, 15 to 86400 (default 300)"`
	StaticData  string `json:"static_data,omitempty" jsonschema:"static: the JSON to show"`
	PerUser     bool   `json:"per_user,omitempty" jsonschema:"push: personal, so everyone who installs the published app sends their own data"`
	Example     string `json:"example,omitempty" jsonschema:"personal: example JSON, in the shape scripts send, that App Store previews show people who haven't sent their own"`
}

func (s *server) createSource(ctx context.Context, req *mcp.CallToolRequest, in createSourceIn) (*mcp.CallToolResult, sourceView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, sourceView{}, err
	}
	if in.Kind == "" {
		in.Kind = "http"
	}
	if in.Kind == "http" && !s.lim.Allow("fetch:"+c.userID, 30, time.Minute) {
		return nil, sourceView{}, errors.New("too many requests: wait a minute")
	}
	src, err := s.Library.SaveSource(ctx, c.userID, "", library.SourceInput{Name: in.Name, Kind: in.Kind, Method: in.Method,
		URL: in.URL, Body: in.Body, IntervalSec: in.IntervalSec, StaticData: in.StaticData, PerUser: in.PerUser, Example: in.Example}, nil)
	if err != nil {
		return nil, sourceView{}, fail(err, "data source")
	}
	return nil, s.sourceView(src), nil
}

type updateSourceIn struct {
	SourceID    string  `json:"source_id"`
	Name        *string `json:"name,omitempty"`
	Method      *string `json:"method,omitempty"`
	URL         *string `json:"url,omitempty"`
	Body        *string `json:"body,omitempty"`
	IntervalSec *int    `json:"interval_sec,omitempty"`
	StaticData  *string `json:"static_data,omitempty"`
	PerUser     *bool   `json:"per_user,omitempty" jsonschema:"push: personal, so everyone who installs the published app sends their own data"`
	Example     *string `json:"example,omitempty" jsonschema:"personal: example JSON that App Store previews show people who haven't sent their own"`
}

func (s *server) updateSource(ctx context.Context, req *mcp.CallToolRequest, in updateSourceIn) (*mcp.CallToolResult, sourceView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, sourceView{}, err
	}
	src, err := s.Store.UserSource(ctx, c.userID, in.SourceID)
	if err != nil {
		return nil, sourceView{}, fail(err, "data source")
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	si := library.SourceInput{Name: src.Name, Kind: src.Kind, Method: src.Method, URL: src.URL, Body: src.Body,
		IntervalSec: src.IntervalSec, StaticData: src.LastData, PerUser: src.PerUser, Example: src.Example}
	if in.PerUser != nil {
		si.PerUser = *in.PerUser
	}
	set(&si.Name, in.Name)
	set(&si.Method, in.Method)
	set(&si.URL, in.URL)
	set(&si.Body, in.Body)
	set(&si.StaticData, in.StaticData)
	set(&si.Example, in.Example)
	if in.IntervalSec != nil {
		si.IntervalSec = *in.IntervalSec
	}
	src, err = s.Library.SaveSource(ctx, c.userID, src.ID, si, nil)
	if err != nil {
		return nil, sourceView{}, fail(err, "data source")
	}
	return nil, s.sourceView(src), nil
}

func (s *server) fetchSource(ctx context.Context, req *mcp.CallToolRequest, in sourceID) (*mcp.CallToolResult, sourceView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, sourceView{}, err
	}
	if !s.lim.Allow("fetch:"+c.userID, 30, time.Minute) {
		return nil, sourceView{}, errors.New("too many requests: wait a minute")
	}
	src, err := s.Library.FetchSource(ctx, c.userID, in.SourceID)
	if err != nil {
		return nil, sourceView{}, fail(err, "data source")
	}
	return nil, s.sourceView(src), nil
}

// ---- apps ----

type appID struct {
	AppID string `json:"app_id"`
}

func (s *server) getApp(ctx context.Context, req *mcp.CallToolRequest, in appID) (*mcp.CallToolResult, appView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, appView{}, err
	}
	a, err := s.Store.UserApp(ctx, c.userID, in.AppID)
	if err != nil {
		return nil, appView{}, fail(err, "app")
	}
	return nil, s.appView(ctx, c.userID, a), nil
}

type createAppIn struct {
	Name        string `json:"name" jsonschema:"shown in the board's Apps drawer; up to 40 characters, shorter reads better"`
	Description string `json:"description,omitempty" jsonschema:"what it shows, in a sentence"`
	CycleSec    int    `json:"cycle_sec,omitempty" jsonschema:"turn to the next screen after this many seconds without a button press; 0 = off"`
}

func (s *server) createApp(ctx context.Context, req *mcp.CallToolRequest, in createAppIn) (*mcp.CallToolResult, appView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, appView{}, err
	}
	a, err := s.Library.CreateApp(ctx, c.userID, in.Name, "", c.client)
	if err != nil {
		return nil, appView{}, fail(err, "app")
	}
	if in.Description != "" || in.CycleSec != 0 {
		if a, err = s.Library.UpdateApp(ctx, c.userID, a.ID, library.AppChanges{Description: &in.Description, CycleSec: &in.CycleSec}); err != nil {
			return nil, appView{}, fail(err, "app")
		}
	}
	return nil, s.appView(ctx, c.userID, a), nil
}

type updateAppIn struct {
	AppID       string  `json:"app_id"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	CycleSec    *int    `json:"cycle_sec,omitempty" jsonschema:"seconds between automatic page turns; 0 = off"`
}

func (s *server) updateApp(ctx context.Context, req *mcp.CallToolRequest, in updateAppIn) (*mcp.CallToolResult, appView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, appView{}, err
	}
	a, err := s.Library.UpdateApp(ctx, c.userID, in.AppID, library.AppChanges{Name: in.Name, Description: in.Description, CycleSec: in.CycleSec})
	if err != nil {
		return nil, appView{}, fail(err, "app")
	}
	return nil, s.appView(ctx, c.userID, a), nil
}

// ---- screens ----

// screenFields are a screen's settings; in updates, only the fields given change.
type screenFields struct {
	Name       *string `json:"name,omitempty" jsonschema:"the screen's name in makerApp; defaults to the title"`
	Layout     *string `json:"layout,omitempty" jsonschema:"big_number (default), list, text, spark, progress or gif"`
	SourceID   *string `json:"source_id,omitempty" jsonschema:"the data source its templates read; empty for fixed text"`
	Title      *string `json:"title,omitempty" jsonschema:"header text (template)"`
	Value      *string `json:"value,omitempty" jsonschema:"big_number, spark, progress: the main value (template)"`
	Sub        *string `json:"sub,omitempty" jsonschema:"big_number, progress: the subtitle (template)"`
	SubColor   *string `json:"sub_color,omitempty" jsonschema:"big_number: empty for gray, auto (green for +, red for -), or #RRGGBB"`
	Body       *string `json:"body,omitempty" jsonschema:"text: the text (template)"`
	ItemsPath  *string `json:"items_path,omitempty" jsonschema:"list: path of the list in the data, without braces"`
	ItemLabel  *string `json:"item_label,omitempty" jsonschema:"list: each row's label (template, relative to the item)"`
	ItemValue  *string `json:"item_value,omitempty" jsonschema:"list: each row's value (template, relative to the item)"`
	MaxItems   *int    `json:"max_items,omitempty" jsonschema:"list: the most rows, 1 to 30 (default 10)"`
	SeriesPath *string `json:"series_path,omitempty" jsonschema:"spark: path of a list of numbers, without braces"`
	PctValue   *string `json:"pct_value,omitempty" jsonschema:"progress: the amount so far (template)"`
	PctMax     *string `json:"pct_max,omitempty" jsonschema:"progress: the target (template)"`
	GIF        *string `json:"gif,omitempty" jsonschema:"gif: the ID of a GIF uploaded in makerApp"`
	Accent     *string `json:"accent,omitempty" jsonschema:"#RRGGBB color of the title and highlights"`
	BG         *string `json:"bg,omitempty" jsonschema:"#RRGGBB background color"`
}

// apply sets the given fields on a screen's name, layout, source and spec.
func (f screenFields) apply(name, layout, source *string, spec *render.Spec) error {
	for _, kv := range []struct {
		dst *string
		v   *string
	}{{name, f.Name}, {layout, f.Layout}, {source, f.SourceID}, {&spec.Title, f.Title}, {&spec.Value, f.Value},
		{&spec.Sub, f.Sub}, {&spec.SubColor, f.SubColor}, {&spec.Body, f.Body}, {&spec.ItemsPath, f.ItemsPath},
		{&spec.ItemLabel, f.ItemLabel}, {&spec.ItemValue, f.ItemValue}, {&spec.SeriesPath, f.SeriesPath},
		{&spec.PctValue, f.PctValue}, {&spec.PctMax, f.PctMax}, {&spec.GIF, f.GIF}, {&spec.Accent, f.Accent}, {&spec.BG, f.BG}} {
		if kv.v != nil {
			*kv.dst = *kv.v
		}
	}
	if f.MaxItems != nil {
		spec.MaxItems = *f.MaxItems
	}
	if *layout == "" {
		*layout = "big_number"
	}
	if !slices.Contains(render.Layouts, *layout) {
		return fmt.Errorf("layout must be one of %s", strings.Join(render.Layouts, ", "))
	}
	return nil
}

type previewIn struct {
	AppID string `json:"app_id,omitempty" jsonschema:"the app it would go in, so the checks count its screens"`
	screenFields
}

func (s *server) previewScreen(ctx context.Context, req *mcp.CallToolRequest, in previewIn) (*mcp.CallToolResult, screenView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, screenView{}, err
	}
	var name, layout, source string
	spec := render.DefaultSpec("")
	if err := in.apply(&name, &layout, &source, &spec); err != nil {
		return nil, screenView{}, err
	}
	out, err := s.Library.Preview(ctx, c.userID, library.ScreenInput{Name: name, Layout: layout, Spec: spec, SourceID: source})
	if err != nil {
		return nil, screenView{}, fail(err, "screen")
	}
	screens := 1
	if in.AppID != "" {
		list, _ := s.Store.AppScreens(ctx, in.AppID)
		screens = len(list) + 1
	}
	return nil, s.screenView(ctx, "", name, layout, source, spec, out, screens), nil
}

type addScreenIn struct {
	AppID string `json:"app_id" jsonschema:"the app to add the screen to (create_app makes one)"`
	screenFields
}

func (s *server) addScreen(ctx context.Context, req *mcp.CallToolRequest, in addScreenIn) (*mcp.CallToolResult, screenView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, screenView{}, err
	}
	if in.AppID == "" {
		return nil, screenView{}, errors.New("app_id is required: create an app with create_app first")
	}
	var name, layout, source string
	spec := render.DefaultSpec("")
	if err := in.apply(&name, &layout, &source, &spec); err != nil {
		return nil, screenView{}, err
	}
	sc, err := s.Library.SaveScreen(ctx, c.userID, "", library.ScreenInput{App: in.AppID, Name: name, Layout: layout,
		Spec: spec, SourceID: source, MadeBy: c.client})
	if err != nil {
		return nil, screenView{}, fail(err, "app")
	}
	return nil, s.savedScreen(ctx, sc), nil
}

type updateScreenIn struct {
	ScreenID string `json:"screen_id"`
	screenFields
}

func (s *server) updateScreen(ctx context.Context, req *mcp.CallToolRequest, in updateScreenIn) (*mcp.CallToolResult, screenView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, screenView{}, err
	}
	sc, err := s.Store.UserScreen(ctx, c.userID, in.ScreenID)
	if err != nil {
		return nil, screenView{}, fail(err, "screen")
	}
	name, layout, source := sc.Name, sc.Layout, sc.SourceID
	spec := render.ParseSpec(sc.Layout, sc.Spec)
	if err := in.apply(&name, &layout, &source, &spec); err != nil {
		return nil, screenView{}, err
	}
	sc, err = s.Library.SaveScreen(ctx, c.userID, sc.ID, library.ScreenInput{Name: name, Layout: layout, Spec: spec, SourceID: source})
	if err != nil {
		return nil, screenView{}, fail(err, "screen")
	}
	return nil, s.savedScreen(ctx, sc), nil
}

type reorderIn struct {
	AppID     string   `json:"app_id"`
	ScreenIDs []string `json:"screen_ids" jsonschema:"every screen of the app, in the new order"`
}

func (s *server) reorderScreens(ctx context.Context, req *mcp.CallToolRequest, in reorderIn) (*mcp.CallToolResult, appView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, appView{}, err
	}
	if err := s.Library.ReorderScreens(ctx, c.userID, in.AppID, in.ScreenIDs); err != nil {
		return nil, appView{}, fail(err, "app")
	}
	a, err := s.Store.UserApp(ctx, c.userID, in.AppID)
	if err != nil {
		return nil, appView{}, fail(err, "app")
	}
	return nil, s.appView(ctx, c.userID, a), nil
}

// ---- boards ----

type installIn struct {
	AppID   string `json:"app_id" jsonschema:"one of the user's apps, or a built-in app's id (clock, snake, …)"`
	BoardID string `json:"board_id"`
}

func (s *server) installApp(ctx context.Context, req *mcp.CallToolRequest, in installIn) (*mcp.CallToolResult, boardView, error) {
	return s.setInstalled(ctx, req, in, true)
}

func (s *server) uninstallApp(ctx context.Context, req *mcp.CallToolRequest, in installIn) (*mcp.CallToolResult, boardView, error) {
	return s.setInstalled(ctx, req, in, false)
}

func (s *server) setInstalled(ctx context.Context, req *mcp.CallToolRequest, in installIn, on bool) (*mcp.CallToolResult, boardView, error) {
	c, err := s.caller(req)
	if err != nil {
		return nil, boardView{}, err
	}
	d, err := s.Library.SetInstalled(ctx, c.userID, in.BoardID, in.AppID, on, true)
	if err != nil {
		return nil, boardView{}, fail(err, "board or app")
	}
	return nil, s.board(ctx, d), nil
}
