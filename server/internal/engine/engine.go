// Package engine is the live part of espHog: it holds device WebSocket
// connections, polls data sources on their intervals, renders screens and
// pushes them to devices the moment anything changes.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"esphog/internal/fetch"
	"esphog/internal/fw"
	"esphog/internal/games"
	"esphog/internal/render"
	"esphog/internal/secret"
	"esphog/internal/store"
)

type Engine struct {
	st    *store.Store
	box   *secret.Box
	fetch *fetch.Client
	loc   *time.Location
	log   *slog.Logger

	mu    sync.RWMutex
	conns map[string]*conn

	inflightMu sync.Mutex
	inflight   map[string]bool
}

type conn struct {
	id   string
	ws   *websocket.Conn
	send chan []byte
	base string // http(s)://host the device reached us on, for OTA links
	fw   string
}

func New(st *store.Store, box *secret.Box, fc *fetch.Client, loc *time.Location, log *slog.Logger) *Engine {
	return &Engine{st: st, box: box, fetch: fc, loc: loc, log: log,
		conns: map[string]*conn{}, inflight: map[string]bool{}}
}

// ---- secrets on sources ----

type SourceSecret struct {
	Auth    string        `json:"auth"`
	Headers string        `json:"headers"`
	Params  []fetch.Param `json:"params,omitempty"` // secret query parameters
}

func (e *Engine) OpenSecret(src *store.Source) SourceSecret {
	var s SourceSecret
	plain, err := e.box.Open(src.Secret)
	if err == nil && plain != "" {
		_ = json.Unmarshal([]byte(plain), &s)
	}
	return s
}

func (e *Engine) SealSecret(s SourceSecret) string {
	if s.Auth == "" && s.Headers == "" && len(s.Params) == 0 {
		return ""
	}
	b, _ := json.Marshal(s)
	return e.box.Seal(string(b))
}

// ---- connections ----

func (e *Engine) Online(id string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.conns[id] != nil
}

// Send queues a message for a connected device. It reports whether the device is online.
func (e *Engine) Send(id string, msg any) bool {
	b, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	e.mu.RLock()
	c := e.conns[id]
	e.mu.RUnlock()
	if c == nil {
		return false
	}
	select {
	case c.send <- b:
	default:
		e.log.Warn("device send queue full", "device", id)
	}
	return true
}

func (e *Engine) Toast(id, text string) bool {
	return e.Send(id, map[string]any{"type": "toast", "text": render.ASCII(text)})
}

// OTA asks the device to download the firmware embedded in this server.
func (e *Engine) OTA(id string) bool {
	e.mu.RLock()
	c := e.conns[id]
	e.mu.RUnlock()
	if c == nil {
		return false
	}
	return e.Send(id, map[string]any{"type": "ota", "url": c.base + "/fw/" + fw.Hash() + "/firmware.bin"})
}

type hello struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Token string `json:"token"`
	Code  string `json:"code"`
	FW    string `json:"fw"`
	Board string `json:"board"`
	IP    string `json:"ip"`
	RSSI  int    `json:"rssi"`
	Claim string `json:"claim"`
}

type inbound struct {
	Type string `json:"type"`
}

// HandleWS is the device endpoint (/ws).
func (e *Engine) HandleWS(w http.ResponseWriter, r *http.Request) {
	// Devices aren't browsers: the Arduino client sends a placeholder Origin
	// header, so the browser-oriented origin check is off here. Devices
	// authenticate with their secret in the hello message instead.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{"esphog", "arduino"},
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer ws.CloseNow()

	helloCtx, helloCancel := context.WithTimeout(ctx, 20*time.Second)
	_, raw, err := ws.Read(helloCtx)
	helloCancel()
	if err != nil {
		return
	}
	var h hello
	if json.Unmarshal(raw, &h) != nil || h.Type != "hello" || !validDeviceID(h.ID) || len(h.Token) < 16 {
		ws.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}

	c := &conn{id: h.ID, ws: ws, send: make(chan []byte, 32), base: baseURL(r), fw: h.FW}
	reply, err := e.admit(ctx, &h)
	if err != nil {
		e.log.Info("device rejected", "device", h.ID, "err", err)
		b, _ := json.Marshal(map[string]any{"type": "error", "text": err.Error()})
		_ = ws.Write(ctx, websocket.MessageText, b)
		ws.Close(websocket.StatusPolicyViolation, "rejected")
		return
	}

	e.mu.Lock()
	if old := e.conns[h.ID]; old != nil {
		old.ws.Close(websocket.StatusGoingAway, "replaced")
	}
	e.conns[h.ID] = c
	e.mu.Unlock()
	e.log.Info("device connected", "device", h.ID, "fw", h.FW, "ip", h.IP)
	defer func() {
		e.mu.Lock()
		if e.conns[h.ID] == c {
			delete(e.conns, h.ID)
		}
		e.mu.Unlock()
		_ = e.st.TouchDevice(context.Background(), h.ID)
		e.log.Info("device disconnected", "device", h.ID)
	}()

	go e.writeLoop(ctx, cancel, c)
	go e.pingLoop(ctx, cancel, c)
	for _, m := range reply {
		e.Send(h.ID, m)
	}

	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m inbound
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		switch m.Type {
		case "hello":
			// Device re-sent hello (e.g. after being unpaired); re-admit it.
			var h2 hello
			if json.Unmarshal(raw, &h2) == nil && h2.ID == h.ID {
				if msgs, err := e.admit(ctx, &h2); err == nil {
					for _, mm := range msgs {
						e.Send(h.ID, mm)
					}
				}
			}
		}
	}
}

func (e *Engine) writeLoop(ctx context.Context, cancel context.CancelFunc, c *conn) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.send:
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(wctx, websocket.MessageText, b)
			wcancel()
			if err != nil {
				return
			}
		}
	}
}

func (e *Engine) pingLoop(ctx context.Context, cancel context.CancelFunc, c *conn) {
	defer cancel()
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
			_ = e.st.TouchDevice(ctx, c.id)
		}
	}
}

// admit authenticates a hello and returns the messages to send back.
//
// A device proves itself with the secret it generated on first boot (trust on
// first use). A claimed device with a different secret is refused, unless it
// also carries a claim token from its owner (it was re-flashed from the site).
func (e *Engine) admit(ctx context.Context, h *hello) ([]any, error) {
	existing, err := e.st.Device(ctx, h.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	claimOwner := ""
	if h.Claim != "" {
		claimOwner, _ = e.st.UseClaim(ctx, h.Claim, h.ID)
	}
	if existing != nil && existing.UserID != "" && existing.TokenHash != store.HashToken(h.Token) {
		if claimOwner != existing.UserID {
			return nil, errors.New("Registered to another account. Remove it there, then retry.")
		}
	}
	d := &store.Device{ID: h.ID, TokenHash: store.HashToken(h.Token), PairCode: h.Code, FWVersion: h.FW,
		Board: h.Board, IP: h.IP, RSSI: h.RSSI}
	if err := e.st.UpsertDeviceHello(ctx, d); err != nil {
		return nil, err
	}
	owner := ""
	if existing != nil {
		owner = existing.UserID
	}
	if owner == "" && claimOwner != "" {
		if err := e.st.ClaimDeviceFor(ctx, h.ID, claimOwner, DefaultName(h.ID)); err != nil {
			return nil, err
		}
		owner = claimOwner
		e.log.Info("device claimed via flash", "device", h.ID)
	}
	if owner == "" {
		return []any{map[string]any{"type": "pair", "code": h.Code}}, nil
	}
	state, err := e.BuildState(ctx, h.ID)
	if err != nil {
		return nil, err
	}
	return []any{state}, nil
}

func DefaultName(id string) string { return "makerDevice " + id[len(id)-4:] }

func validDeviceID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789ABCDEF", r) {
			return false
		}
	}
	return true
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// ---- apps ----

// CatalogApp is an app made for makerApp itself (games included) that anyone
// can install, shown with its publisher; apps people publish are listed beside
// them (store.PublishedApps). Unlike your own apps it isn't edited here. Devices install it by ID; the board
// either draws it itself (the clock, and the games built into 2.x firmware) or
// downloads it as a Lua script from internal/games.
type CatalogApp struct {
	ID, Name, Desc string
	Publisher      string // shown as @Publisher
	Native         string // first firmware that has it built in; "" if none does
	Lua            string // first firmware that downloads it as a Lua script; "" if it isn't one
}

// Makers publishes the apps made for makerApp itself.
const Makers = "makerInc"

var Catalog = []CatalogApp{
	{ID: "clock", Name: "Clock", Publisher: Makers, Native: "1.0",
		Desc: "Time and date from the board's own clock. Keeps ticking offline."},
	{ID: "pomodoro", Name: "Pomodoro", Publisher: Makers, Lua: "3.1",
		Desc: "25 minutes of focus, then a break; a long one every fourth time. Keeps counting while you use other apps."},
	{ID: "flappy", Name: "Flappy Hog", Publisher: Makers, Native: "2.1", Lua: "3.0",
		Desc: "Flap through the pipes; any button flaps. From PostHog's DeskHog."},
	{ID: "paddle", Name: "Paddle", Publisher: Makers, Native: "2.1", Lua: "3.0",
		Desc: "Pong against the board, first to 5. Top and bottom move, middle starts or pauses. From PostHog's DeskHog."},
	{ID: "runner", Name: "Hog Runner", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Jump the cacti, duck the birds; it keeps speeding up. Middle jumps, bottom ducks."},
	{ID: "snake", Name: "Snake", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Eat and grow without hitting a wall or your tail. Top turns left, bottom turns right, middle pauses."},
	{ID: "breakout", Name: "Breakout", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Clear the wall of bricks. Top and bottom move the paddle, middle launches the ball. Three lives."},
	{ID: "whack", Name: "Whack-a-Hog", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Hogs pop up in three rows; press that row's button before they hide. 30 seconds a round."},
	{ID: "simon", Name: "Simon", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Watch the pattern on the three pads, then repeat it. One step longer every round."},
	{ID: "stacker", Name: "Stacker", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Drop the sliding row onto the stack; overhangs fall off. Reach the top."},
	{ID: "shooter", Name: "Hog Invaders", Publisher: Makers, Native: "2.3", Lua: "3.0",
		Desc: "Space Invaders on its side. Top and bottom move your ship, middle fires. Three lives."},
}

// RunsOn reports whether a board on this firmware version can show the app.
func (a CatalogApp) RunsOn(firmware string) bool {
	return (a.Lua != "" && firmwareAtLeast(firmware, a.Lua)) || (a.Native != "" && firmwareAtLeast(firmware, a.Native))
}

// NeedsFirmware is the version to update to when a board can't show the app.
func (a CatalogApp) NeedsFirmware() string {
	if a.Lua != "" {
		return a.Lua
	}
	return a.Native
}

// deviceScreen is what a board on this firmware gets for the app: a Lua app to
// download, or a layout the firmware draws itself.
func (a CatalogApp) deviceScreen(firmware string) render.Screen {
	if a.Lua != "" && firmwareAtLeast(firmware, a.Lua) {
		return render.Screen{ID: a.ID, Layout: "game", Game: a.ID, Hash: games.Hash(a.ID)}
	}
	return render.Screen{ID: a.ID, Layout: a.ID}
}

func CatalogEntry(id string) (CatalogApp, bool) {
	for _, a := range Catalog {
		if a.ID == id {
			return a, true
		}
	}
	return CatalogApp{}, false
}

// CatalogPreview is how a catalog app looks on the website.
func (e *Engine) CatalogPreview(id string) render.Screen {
	sc := render.Screen{ID: id, Layout: id}
	if id == "clock" {
		now := time.Now().In(e.loc)
		sc.Value, sc.Sub = now.Format("15:04"), now.Format("Mon, Jan 02")
	}
	return sc
}

// AppView is an installed app with its screens resolved.
type AppView struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Cycle   int             `json:"cycle,omitempty"` // seconds between automatic page turns
	Screens []render.Screen `json:"screens"`
	Catalog bool            `json:"-"` // someone else's: the catalog's, or published by another user
	By      string          `json:"-"` // its publisher, for those
}

// DeviceApps resolves the apps installed on a device, in drawer order. With
// preview set, screens carry sample device values for the website.
func (e *Engine) DeviceApps(ctx context.Context, dev *store.Device, preview bool) []AppView {
	ids, _ := e.st.DeviceAppIDs(ctx, dev.ID)
	cache := map[string]*store.Source{}
	out := []AppView{}
	for _, id := range ids {
		if c, ok := CatalogEntry(id); ok {
			sc := c.deviceScreen(dev.FWVersion)
			if preview {
				sc = e.CatalogPreview(c.ID)
			}
			out = append(out, AppView{ID: c.ID, Name: c.Name, Catalog: true, By: c.Publisher, Screens: []render.Screen{sc}})
			continue
		}
		a, by, err := e.st.InstallableApp(ctx, dev.UserID, id)
		if err != nil {
			continue // deleted, or unpublished by someone else
		}
		v := AppView{ID: a.ID, Name: a.Name, Cycle: a.CycleSec, Screens: []render.Screen{},
			Catalog: a.UserID != dev.UserID, By: by}
		screens, _ := e.st.AppScreens(ctx, a.ID)
		for _, sc := range screens {
			v.Screens = append(v.Screens, e.renderScreen(ctx, sc, cache, preview))
		}
		out = append(out, v)
	}
	return out
}

// ---- rendering & pushing ----

// RenderScreen resolves a screen for the device: {{ device.* }} fields stay as
// markers the board fills in itself.
func (e *Engine) RenderScreen(ctx context.Context, s *store.Screen, cache map[string]*store.Source) render.Screen {
	return e.renderScreen(ctx, s, cache, false)
}

// PreviewScreen resolves a screen for the website, with sample device values.
func (e *Engine) PreviewScreen(ctx context.Context, s *store.Screen, cache map[string]*store.Source) render.Screen {
	return e.renderScreen(ctx, s, cache, true)
}

// PreviewScreens is PreviewScreen for a list.
func (e *Engine) PreviewScreens(ctx context.Context, screens []*store.Screen) []render.Screen {
	cache := map[string]*store.Source{}
	out := make([]render.Screen, 0, len(screens))
	for _, s := range screens {
		out = append(out, e.PreviewScreen(ctx, s, cache))
	}
	return out
}

func (e *Engine) renderScreen(ctx context.Context, s *store.Screen, cache map[string]*store.Source, preview bool) render.Screen {
	rc := render.Context{Loc: e.loc, Preview: preview}
	var srcErr string
	if s.SourceID != "" {
		src, ok := cache[s.SourceID]
		if !ok {
			src, _ = e.st.Source(ctx, s.SourceID)
			if cache != nil {
				cache[s.SourceID] = src
			}
		}
		if src != nil {
			rc.Data = src.LastData
			if src.LastFetchedAt > 0 {
				rc.FetchedAt = time.Unix(src.LastFetchedAt, 0)
			}
			srcErr = src.LastError
		}
	}
	out := render.Render(s.ID, s.Name, s.Layout, render.ParseSpec(s.Layout, s.Spec), rc)
	out.Error = srcErr
	return out
}

// MaxStateBytes is the largest "state" message a board accepts: the WebSockets
// library on the board drops the connection over 15 KB, and the board keeps
// reconnecting into the same message. Long lists and texts fill it fastest.
const MaxStateBytes = 15 * 1024

// StateSize is how big the device's "state" message is right now, in bytes.
func (e *Engine) StateSize(ctx context.Context, deviceID string) int {
	state, err := e.BuildState(ctx, deviceID)
	if err != nil {
		return 0
	}
	b, _ := json.Marshal(state)
	return len(b)
}

// BuildState is the "state" message a device runs: its installed apps, in
// drawer order, plus display settings.
func (e *Engine) BuildState(ctx context.Context, deviceID string) (map[string]any, error) {
	dev, err := e.st.Device(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	apps := []AppView{}
	for _, a := range e.DeviceApps(ctx, dev, false) {
		if c, ok := CatalogEntry(a.ID); ok && !c.RunsOn(dev.FWVersion) {
			continue // shows up once the board updates
		}
		a.Name = render.ASCII(a.Name)
		if len(a.Screens) == 0 {
			a.Screens = []render.Screen{{ID: "empty", Layout: "text", Title: a.Name,
				Body: "No screens yet. Add some in makerApp."}}
		}
		apps = append(apps, a)
	}
	_, offset := time.Now().In(e.loc).Zone()
	state := map[string]any{
		"type":       "state",
		"name":       render.ASCII(dev.Name),
		"brightness": dev.Brightness,
		"flip":       dev.Flipped,
		"ox":         dev.OffsetX,
		"oy":         dev.OffsetY,
		"tz":         offset,
	}
	if legacyFirmware(dev.FWVersion) {
		addLegacyFlow(state, apps)
	} else {
		state["apps"] = apps
	}
	return state, nil
}

// legacyFirmware reports firmware older than 2.0, which runs a flow of nodes
// instead of apps.
func legacyFirmware(version string) bool { return !firmwareAtLeast(version, "2.0") }

// firmwareAtLeast compares major.minor versions. Versions that don't parse
// (development builds) count as new.
func firmwareAtLeast(have, want string) bool {
	parse := func(v string) (int, int, bool) {
		major, rest, _ := strings.Cut(v, ".")
		minor, _, _ := strings.Cut(rest, ".")
		a, err1 := strconv.Atoi(major)
		b, err2 := strconv.Atoi(minor)
		return a, b, err1 == nil && (err2 == nil || minor == "")
	}
	ha, hb, ok := parse(have)
	if !ok {
		return true
	}
	wa, wb, _ := parse(want)
	return ha > wa || (ha == wa && hb >= wb)
}

// addLegacyFlow lets a pre-2.0 board step through every installed screen with
// D0/D2 until it updates.
func addLegacyFlow(state map[string]any, apps []AppView) {
	nodes := []map[string]any{}
	screens := []render.Screen{}
	for _, a := range apps {
		for i, sc := range a.Screens {
			n := map[string]any{"id": a.ID + "." + strconv.Itoa(i), "kind": "screen", "group": "all", "screen": sc,
				"b": []map[string]string{{"a": "prev"}, {"a": "none"}, {"a": "next"}}}
			if sc.Layout == "clock" {
				n["kind"] = "clock"
			} else {
				screens = append(screens, sc)
			}
			nodes = append(nodes, n)
		}
	}
	state["nodes"], state["screens"] = nodes, screens
	if len(nodes) > 0 {
		state["home"] = nodes[0]["id"]
	}
}

func (e *Engine) PushDevice(ctx context.Context, id string) {
	if !e.Online(id) {
		return
	}
	state, err := e.BuildState(ctx, id)
	if err != nil {
		e.log.Error("build state", "device", id, "err", err)
		return
	}
	if b, _ := json.Marshal(state); len(b) > MaxStateBytes {
		e.log.Warn("state too big for the board", "device", id, "bytes", len(b))
	}
	e.Send(id, state)
}

// PushScreens re-sends state to every device with an app containing these screens.
func (e *Engine) PushScreens(ctx context.Context, screenIDs ...string) {
	ids, err := e.st.DevicesForScreens(ctx, screenIDs)
	if err != nil {
		e.log.Error("devices for screens", "err", err)
		return
	}
	for _, id := range ids {
		e.PushDevice(ctx, id)
	}
}

// PushApp re-sends state to every device the app is installed on.
func (e *Engine) PushApp(ctx context.Context, appID string) {
	ids, _ := e.st.DevicesWithApp(ctx, appID)
	for _, id := range ids {
		e.PushDevice(ctx, id)
	}
}

// DevicesShowing lists devices with an app installed that contains the screen.
func (e *Engine) DevicesShowing(ctx context.Context, screenID string) []string {
	ids, _ := e.st.DevicesForScreens(ctx, []string{screenID})
	return ids
}

func (e *Engine) PushSource(ctx context.Context, sourceID string) {
	ids, err := e.st.ScreenIDsForSource(ctx, sourceID)
	if err != nil || len(ids) == 0 {
		return
	}
	e.PushScreens(ctx, ids...)
}

// ---- data sources ----

// Run polls HTTP sources whose interval has elapsed until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	sem := make(chan struct{}, 8)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		due, err := e.st.DueSources(ctx)
		if err != nil {
			e.log.Error("due sources", "err", err)
			continue
		}
		for _, src := range due {
			if !e.begin(src.ID) {
				continue
			}
			sem <- struct{}{}
			go func(src *store.Source) {
				defer func() { <-sem; e.end(src.ID) }()
				_ = e.refresh(ctx, src)
			}(src)
		}
	}
}

func (e *Engine) begin(id string) bool {
	e.inflightMu.Lock()
	defer e.inflightMu.Unlock()
	if e.inflight[id] {
		return false
	}
	e.inflight[id] = true
	return true
}

func (e *Engine) end(id string) {
	e.inflightMu.Lock()
	delete(e.inflight, id)
	e.inflightMu.Unlock()
}

// Refresh fetches a source now and pushes the result to devices showing it.
func (e *Engine) Refresh(ctx context.Context, src *store.Source) error {
	if src.Kind != "http" {
		e.PushSource(ctx, src.ID)
		return nil
	}
	if !e.begin(src.ID) {
		return errors.New("already refreshing")
	}
	defer e.end(src.ID)
	return e.refresh(ctx, src)
}

func (e *Engine) refresh(ctx context.Context, src *store.Source) error {
	sec := e.OpenSecret(src)
	fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := e.fetch.Do(fctx, fetch.Request{Method: src.Method, URL: src.URL, Query: sec.Params, Auth: sec.Auth, Headers: sec.Headers, Body: src.Body})
	status := 0
	if res != nil {
		status = res.Status
	}
	if err != nil {
		_ = e.st.SetSourceResult(ctx, src.ID, "", status, err.Error())
		e.log.Info("source fetch failed", "source", src.ID, "err", err)
		return err
	}
	if err := e.st.SetSourceResult(ctx, src.ID, res.Data, status, ""); err != nil {
		return err
	}
	if res.Data != src.LastData {
		e.PushSource(ctx, src.ID)
	}
	return nil
}

// SetPushedData stores data posted to a push source and syncs devices.
func (e *Engine) SetPushedData(ctx context.Context, src *store.Source, data string) error {
	if err := e.st.SetSourceResult(ctx, src.ID, data, 200, ""); err != nil {
		return err
	}
	e.PushSource(ctx, src.ID)
	return nil
}
