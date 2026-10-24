// Package library holds the rules for changing what a user has built: data
// sources, apps and their screens, and which apps are on which board. The
// website's API and the MCP tools both go through it, so they check things the
// same way and boards hear about every change.
package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/render"
	"makerapp/internal/store"
)

type Library struct {
	st  *store.Store
	eng *engine.Engine
}

func New(st *store.Store, eng *engine.Engine) *Library { return &Library{st: st, eng: eng} }

// Error is a problem with what was asked for. Its message is meant for the
// user (or the AI acting for them).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func invalid(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// Per-account caps, so a runaway script or AI can't fill the database.
const (
	MaxSources = 100
	MaxApps    = 100
	MaxScreens = 500
)

// ---- data sources ----

type SourceInput struct {
	Name        string
	Kind        string // http | static | push
	Method      string
	URL         string
	Body        string
	IntervalSec int    // 0 means the default, 5 minutes
	StaticData  string // kind "static": the JSON (or text) to show
}

// SourceSecrets are the encrypted parts of a request. Only the website sends
// them: API keys never go through an AI chat.
type SourceSecrets struct {
	Headers   []fetch.Param // header rows, in order
	Params    []fetch.Param // secret query parameters; an empty value keeps the saved one
	Auth      string        // a new token; "" keeps the saved one
	ClearAuth bool
}

// SaveSource creates (id "") or updates a data source. A request is fetched
// right away so its data is ready to map onto screens; the returned source
// carries the result.
func (l *Library) SaveSource(ctx context.Context, userID, id string, in SourceInput, secrets *SourceSecrets) (*store.Source, error) {
	src := &store.Source{UserID: userID}
	if id != "" {
		var err error
		if src, err = l.st.UserSource(ctx, userID, id); err != nil {
			return nil, err
		}
	} else if list, err := l.st.UserSources(ctx, userID); err != nil {
		return nil, err
	} else if len(list) >= MaxSources {
		return nil, invalid("An account can have %d data sources. Delete one first.", MaxSources)
	}
	sec := l.eng.OpenSecret(src)
	if secrets != nil {
		applySecrets(&sec, secrets)
	}
	src.Name = strings.TrimSpace(in.Name)
	src.Kind = in.Kind
	src.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	src.URL = stripParams(strings.TrimSpace(in.URL), sec.Params)
	src.Body = strings.TrimSpace(in.Body)
	if in.IntervalSec == 0 {
		in.IntervalSec = 300
	}
	src.IntervalSec = clamp(in.IntervalSec, 15, 86400)
	src.Secret = l.eng.SealSecret(sec)

	if src.Name == "" {
		return nil, invalid("Give the source a name.")
	}
	switch src.Kind {
	case "http":
		if !strings.HasPrefix(src.URL, "http://") && !strings.HasPrefix(src.URL, "https://") {
			return nil, invalid("The URL must start with https:// (or http://).")
		}
		if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH"}, src.Method) {
			src.Method = "GET"
		}
		if src.Body != "" && src.Method != "GET" && !json.Valid([]byte(src.Body)) {
			return nil, invalid("The request body isn't valid JSON.")
		}
	case "static":
		src.LastData = fetch.ToJSON([]byte(strings.TrimSpace(in.StaticData)))
		src.LastFetchedAt = time.Now().Unix()
		src.LastError = ""
	case "push":
		if src.PushToken == "" {
			src.PushToken = store.RandomToken(16)
		}
	default:
		return nil, invalid("Pick a source type: http, static or push.")
	}
	if err := l.st.SaveSource(ctx, src); err != nil {
		return nil, invalid("Could not save: %v", err)
	}
	if src.Kind == "http" {
		return l.FetchSource(ctx, userID, src.ID)
	}
	l.eng.PushSource(ctx, src.ID)
	return src, nil
}

func applySecrets(sec *engine.SourceSecret, in *SourceSecrets) {
	// Header rows become "Name: value" lines.
	var lines []string
	for _, h := range in.Headers {
		if k := strings.TrimSpace(h.K); k != "" {
			lines = append(lines, k+": "+strings.TrimSpace(h.V))
		}
	}
	sec.Headers = strings.Join(lines, "\n")
	// Secret query parameters stay out of the URL and are added when fetching.
	old := map[string]string{}
	for _, p := range sec.Params {
		old[p.K] = p.V
	}
	sec.Params = nil
	for _, p := range in.Params {
		k := strings.TrimSpace(p.K)
		if k == "" {
			continue
		}
		v := strings.TrimSpace(p.V)
		if v == "" {
			v = old[k] // blank means "keep the saved value"
		}
		if v != "" {
			sec.Params = append(sec.Params, fetch.Param{K: k, V: v})
		}
	}
	if a := strings.TrimSpace(in.Auth); a != "" {
		sec.Auth = a
	}
	if in.ClearAuth {
		sec.Auth = ""
	}
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

// FetchSource fetches a request now (at most 30 seconds) and returns the
// source with the result. Other kinds come back as they are.
func (l *Library) FetchSource(ctx context.Context, userID, id string) (*store.Source, error) {
	src, err := l.st.UserSource(ctx, userID, id)
	if err != nil || src.Kind != "http" {
		return src, err
	}
	fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	_ = l.eng.Refresh(fctx, src) // the outcome is stored on the source
	cancel()
	return l.st.UserSource(ctx, userID, id)
}

// ---- apps ----

// CreateApp makes an app. With the ID of one of the user's boards it's
// installed there right away.
func (l *Library) CreateApp(ctx context.Context, userID, name, deviceID, madeBy string) (*store.App, error) {
	if apps, err := l.st.UserApps(ctx, userID); err != nil {
		return nil, err
	} else if len(apps) >= MaxApps {
		return nil, invalid("An account can have %d apps. Delete one first.", MaxApps)
	}
	name = limit(strings.TrimSpace(name), 40)
	if name == "" {
		name = "My app"
	}
	a := &store.App{UserID: userID, Name: name, MadeBy: madeBy}
	if err := l.st.SaveApp(ctx, a); err != nil {
		return nil, err
	}
	if d, err := l.st.UserDevice(ctx, userID, deviceID); err == nil {
		if err := l.st.InstallApp(ctx, d.ID, a.ID); err != nil {
			return nil, err
		}
		l.eng.PushDevice(ctx, d.ID)
	}
	return a, nil
}

// AppChanges are the app settings to change; nil fields stay as they are.
type AppChanges struct {
	Name        *string
	CycleSec    *int
	Description *string
}

func (l *Library) UpdateApp(ctx context.Context, userID, id string, ch AppChanges) (*store.App, error) {
	a, err := l.st.UserApp(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if ch.Name != nil {
		if n := limit(strings.TrimSpace(*ch.Name), 40); n != "" {
			a.Name = n
		}
	}
	if ch.CycleSec != nil {
		a.CycleSec = clamp(*ch.CycleSec, 0, 3600)
	}
	if ch.Description != nil {
		a.Description = limit(strings.TrimSpace(*ch.Description), 140)
	}
	if err := l.st.SaveApp(ctx, a); err != nil {
		return nil, err
	}
	l.eng.PushApp(ctx, a.ID)
	return a, nil
}

// ---- screens ----

type ScreenInput struct {
	App      string // new screens: the app's ID, or "" / "new" for a new app named after the screen
	Name     string
	Layout   string
	Spec     render.Spec
	SourceID string
	MadeBy   string // the AI client asking, for an app made along the way ("" from the website)
}

// SaveScreen creates (id "") or updates a screen, and updates every board
// that has its app.
func (l *Library) SaveScreen(ctx context.Context, userID, id string, in ScreenInput) (*store.Screen, error) {
	sc := &store.Screen{UserID: userID}
	if id != "" {
		var err error
		if sc, err = l.st.UserScreen(ctx, userID, id); err != nil {
			return nil, err
		}
	} else if screens, err := l.st.UserScreens(ctx, userID); err != nil {
		return nil, err
	} else if len(screens) >= MaxScreens {
		return nil, invalid("An account can have %d screens. Delete one first.", MaxScreens)
	}
	if err := l.prepare(ctx, userID, sc, in); err != nil {
		return nil, err
	}
	if sc.ID == "" {
		if err := l.screenApp(ctx, userID, sc, in.App, in.MadeBy); err != nil {
			return nil, err
		}
	}
	if err := l.st.SaveScreen(ctx, sc); err != nil {
		return nil, invalid("Could not save: %v", err)
	}
	l.eng.PushScreens(ctx, sc.ID)
	return sc, nil
}

// prepare checks a screen's fields and fills in the defaults.
func (l *Library) prepare(ctx context.Context, userID string, sc *store.Screen, in ScreenInput) error {
	if !slices.Contains(render.Layouts, in.Layout) {
		in.Layout = "big_number"
	}
	spec := in.Spec
	if spec.BG == "#000000" {
		spec.BG = ""
	}
	if spec.MaxItems == 0 {
		spec.MaxItems = 10
	}
	spec.MaxItems = clamp(spec.MaxItems, 1, 30)
	spec.ItemsPath = strings.TrimSpace(spec.ItemsPath)
	spec.SeriesPath = strings.TrimSpace(spec.SeriesPath)
	if in.SourceID != "" {
		if _, err := l.st.UserSource(ctx, userID, in.SourceID); err != nil {
			return invalid("That data source doesn't exist.")
		}
	}
	sc.Layout = in.Layout
	sc.SourceID = in.SourceID
	sc.Name = limit(strings.TrimSpace(in.Name), 40)
	if sc.Name == "" {
		sc.Name = limit(spec.Title, 40)
	}
	if sc.Name == "" {
		sc.Name = "Untitled screen"
	}
	b, _ := json.Marshal(spec)
	sc.Spec = string(b)
	return nil
}

// screenApp puts a new screen in the named app. "new" makes an app named after
// the screen, installed on the user's board when they have exactly one.
func (l *Library) screenApp(ctx context.Context, userID string, sc *store.Screen, appID, madeBy string) error {
	if appID != "" && appID != "new" {
		a, err := l.st.UserApp(ctx, userID, appID)
		if err != nil {
			return invalid("That app doesn't exist.")
		}
		sc.AppID = a.ID
		return nil
	}
	a, err := l.CreateApp(ctx, userID, sc.Name, "", madeBy)
	if err != nil {
		return err
	}
	sc.AppID = a.ID
	if devices, _ := l.st.UserDevices(ctx, userID); len(devices) == 1 {
		return l.st.InstallApp(ctx, devices[0].ID, a.ID)
	}
	return nil
}

// Preview renders a screen as the board would show it (with sample values for
// the board's own fields) without saving anything.
func (l *Library) Preview(ctx context.Context, userID string, in ScreenInput) (render.Screen, error) {
	sc := &store.Screen{ID: "preview"}
	if err := l.prepare(ctx, userID, sc, in); err != nil {
		return render.Screen{}, err
	}
	return l.eng.PreviewScreen(ctx, sc, nil), nil
}

// MoveScreen shifts a screen one place earlier (-1) or later (+1) in its app.
func (l *Library) MoveScreen(ctx context.Context, userID, appID, screenID string, delta int) error {
	a, err := l.st.UserApp(ctx, userID, appID)
	if err != nil {
		return err
	}
	if err := l.st.MoveAppScreen(ctx, a.ID, screenID, delta); err != nil {
		return err
	}
	l.eng.PushApp(ctx, a.ID)
	return nil
}

// ReorderScreens puts an app's screens in the given order; ids must be
// exactly the app's screens.
func (l *Library) ReorderScreens(ctx context.Context, userID, appID string, ids []string) error {
	a, err := l.st.UserApp(ctx, userID, appID)
	if err != nil {
		return err
	}
	screens, err := l.st.AppScreens(ctx, a.ID)
	if err != nil {
		return err
	}
	have := make([]string, len(screens))
	for i, sc := range screens {
		have[i] = sc.ID
	}
	want := slices.Clone(ids)
	slices.Sort(have)
	slices.Sort(want)
	if !slices.Equal(have, want) {
		return invalid("List every screen of the app exactly once; it has %d.", len(screens))
	}
	if err := l.st.SetScreenOrder(ctx, a.ID, ids); err != nil {
		return err
	}
	l.eng.PushApp(ctx, a.ID)
	return nil
}

// ---- boards ----

// ErrNotOwnApp: the MCP tools may install only the user's own apps and
// makerApp's built-in ones, not apps other people published.
var ErrNotOwnApp = &Error{Msg: "Only your own apps and makerApp's built-in apps can be installed this way. Install apps other people published from the website."}

// SetInstalled puts an app on one of the user's boards, or takes it off (it
// stays in the library). With ownOnly, apps other people published can't be
// installed. It returns the board.
func (l *Library) SetInstalled(ctx context.Context, userID, deviceID, appID string, on, ownOnly bool) (*store.Device, error) {
	d, err := l.st.UserDevice(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	if on {
		if _, ok := engine.CatalogEntry(appID); !ok {
			a, _, err := l.st.InstallableApp(ctx, userID, appID)
			if err != nil {
				return nil, err
			}
			if ownOnly && a.UserID != userID {
				return nil, ErrNotOwnApp
			}
		}
		err = l.st.InstallApp(ctx, d.ID, appID)
	} else {
		err = l.st.UninstallApp(ctx, d.ID, appID)
	}
	if err != nil {
		return nil, err
	}
	l.eng.PushDevice(ctx, d.ID)
	return d, nil
}

// IsUserError reports whether err is meant for the user (bad input, a cap)
// rather than a server failure.
func IsUserError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

func clamp(n, lo, hi int) int { return max(lo, min(hi, n)) }

// limit cuts s to at most n characters.
func limit(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
