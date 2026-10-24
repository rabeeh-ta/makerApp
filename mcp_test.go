package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/mcpserver"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

// bearer adds an access token to every request, as an MCP client does.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// TestMCP walks what an AI client does: find the sign-in from /mcp's 401,
// register, get the user's consent, trade the code for tokens, then build an
// app with the tools and install it on a (simulated) board.
func TestMCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := store.New(db)
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	fc := fetch.New(true)
	eng := engine.New(st, box, fc, time.UTC, log)
	lib := library.New(st, eng)
	var site *web.Server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { site.Handler().ServeHTTP(w, r) }))
	defer srv.Close()
	site = web.New(web.Config{AllowSignup: true, Fetch: fc,
		MCP: mcpserver.Handler(mcpserver.Deps{Store: st, Engine: eng, Library: lib, Fetch: fc, PublicURL: srv.URL, Log: log})},
		st, eng, lib, log)

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	form := func(c *http.Client, path string, f url.Values, out any) int {
		t.Helper()
		resp, err := c.PostForm(srv.URL+path, f)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}
	getJSON := func(c *http.Client, path string, out any) int {
		t.Helper()
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_ = json.NewDecoder(resp.Body).Decode(out)
		return resp.StatusCode
	}
	form(browser, "/api/signup", url.Values{"email": {"me@example.com"}, "password": {"password123"}}, nil)
	me, _ := st.UserByEmail(ctx, "me@example.com")

	// A board, connected and claimed through a flash, to install onto.
	var claim struct{ Claim string }
	form(browser, "/api/flash/claims", url.Values{}, &claim)
	dev, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{Subprotocols: []string{"makerdevice"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.CloseNow()
	hello, _ := json.Marshal(map[string]any{"type": "hello", "id": "AABBCCDDEEFF", "token": "0123456789abcdef0123", "code": "123456", "fw": "3.1.1", "claim": claim.Claim})
	_ = dev.Write(ctx, websocket.MessageText, hello)
	nextState := func() map[string]any {
		t.Helper()
		for {
			rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
			_, b, err := dev.Read(rctx)
			rcancel()
			if err != nil {
				t.Fatalf("waiting for state: %v", err)
			}
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && m["type"] == "state" {
				return m
			}
		}
	}
	nextState()

	// Discovery: /mcp without a token points at the resource metadata.
	resp, _ := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("unauthenticated /mcp: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	getJSON(http.DefaultClient, "/.well-known/oauth-protected-resource/mcp", &prm)
	if prm.Resource != srv.URL+"/mcp" || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != srv.URL {
		t.Fatalf("resource metadata: %+v", prm)
	}
	var asm struct {
		PKCE []string `json:"code_challenge_methods_supported"`
		CIMD bool     `json:"client_id_metadata_document_supported"`
	}
	getJSON(http.DefaultClient, "/.well-known/oauth-authorization-server", &asm)
	if len(asm.PKCE) != 1 || asm.PKCE[0] != "S256" || !asm.CIMD {
		t.Fatalf("authorization server metadata: %+v", asm)
	}

	// Registration: loopback redirects are fine, other plain http isn't.
	register := func(body string) (int, string) {
		resp, err := http.Post(srv.URL+"/oauth/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r struct{ ClientID string `json:"client_id"` }
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return resp.StatusCode, r.ClientID
	}
	if code, _ := register(`{"client_name":"Bad","redirect_uris":["http://evil.example/cb"]}`); code != 400 {
		t.Fatalf("plain-http redirect registered: %d", code)
	}
	code, clientID := register(`{"client_name":"Claude","redirect_uris":["http://127.0.0.1:9999/callback"]}`)
	if code != 201 || clientID == "" {
		t.Fatalf("register: %d %q", code, clientID)
	}

	// Consent and code. A loopback redirect may use another port.
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	authParams := func(redirect string) url.Values {
		return url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
			"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
			"state": {"xyz"}, "resource": {srv.URL + "/mcp"}}
	}
	var info struct{ Client, Error string }
	if code := getJSON(browser, "/api/oauth/authorize?"+authParams("http://127.0.0.1:5555/callback").Encode(), &info); code != 200 || info.Client != "Claude" {
		t.Fatalf("consent info: %d %+v", code, info)
	}
	if code := getJSON(browser, "/api/oauth/authorize?"+authParams("https://evil.example/cb").Encode(), &info); code != 400 {
		t.Fatalf("an unregistered redirect should be refused: %d", code)
	}
	approve := func() string {
		t.Helper()
		p := authParams("http://127.0.0.1:5555/callback")
		p.Set("decision", "allow")
		var r struct{ Redirect string }
		form(browser, "/api/oauth/authorize", p, &r)
		u, err := url.Parse(r.Redirect)
		if err != nil || u.Query().Get("state") != "xyz" || u.Query().Get("iss") != srv.URL || u.Query().Get("code") == "" {
			t.Fatalf("approval redirect: %q", r.Redirect)
		}
		return u.Query().Get("code")
	}
	type tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Error   string `json:"error"`
	}
	exchange := func(c, v string) tokens {
		var tk tokens
		form(http.DefaultClient, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {c}, "client_id": {clientID},
			"redirect_uri": {"http://127.0.0.1:5555/callback"}, "code_verifier": {v}, "resource": {srv.URL + "/mcp"}}, &tk)
		return tk
	}
	if tk := exchange(approve(), "wrong-verifier-wrong-verifier-wrong-verifier-wrong"); tk.Error != "invalid_grant" {
		t.Fatalf("a wrong PKCE verifier should fail: %+v", tk)
	}
	c := approve()
	tk := exchange(c, verifier)
	if tk.Access == "" || tk.Refresh == "" {
		t.Fatalf("token: %+v", tk)
	}
	if again := exchange(c, verifier); again.Error != "invalid_grant" {
		t.Fatalf("a code works once: %+v", again)
	}

	// The tools, through the SDK's client.
	connect := func(token string) (*mcp.ClientSession, error) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp",
			HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true}, nil)
	}
	session, err := connect(tk.Access)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !strings.Contains(session.InitializeResult().Instructions, "makerDevice") {
		t.Fatal("instructions missing")
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 17 {
		t.Fatalf("tools: %v %d", err, len(tools.Tools))
	}
	call := func(name string, args map[string]any, out any) *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out != nil && !res.IsError {
			b, _ := json.Marshal(res.StructuredContent)
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s: %v %s", name, err, b)
			}
		}
		return res
	}
	errorText := func(res *mcp.CallToolResult) string {
		if !res.IsError || len(res.Content) == 0 {
			return ""
		}
		tc, _ := res.Content[0].(*mcp.TextContent)
		return tc.Text
	}

	var ov struct {
		Boards []struct{ ID, Name string } `json:"boards"`
		BuiltIn []struct{ ID string }      `json:"built_in_apps"`
	}
	call("get_overview", nil, &ov)
	if len(ov.Boards) != 1 || ov.Boards[0].ID != "AABBCCDDEEFF" || len(ov.BuiltIn) == 0 {
		t.Fatalf("overview: %+v", ov)
	}
	var tried struct {
		Status int      `json:"status"`
		Paths  []string `json:"paths"`
	}
	call("try_request", map[string]any{"url": srv.URL + "/api/me"}, &tried)
	if tried.Status != 200 || len(tried.Paths) == 0 {
		t.Fatalf("try_request: %+v", tried)
	}

	var src struct {
		ID    string   `json:"id"`
		Paths []string `json:"paths"`
	}
	call("create_source", map[string]any{"name": "Prices", "kind": "static", "static_data": `{"btc":{"usd":67123.45,"change":2.31}}`}, &src)
	if src.ID == "" || len(src.Paths) == 0 {
		t.Fatalf("create_source: %+v", src)
	}
	var app struct {
		ID     string `json:"id"`
		MadeBy string `json:"made_by"`
	}
	call("create_app", map[string]any{"name": "Crypto", "description": "Bitcoin today"}, &app)
	if app.ID == "" || app.MadeBy != "Claude" {
		t.Fatalf("create_app: %+v", app)
	}
	type screen struct {
		ID    string `json:"id"`
		Shows struct {
			Value, Sub string
		} `json:"shows"`
		Checks []string `json:"checks"`
	}
	var sc screen
	call("add_screen", map[string]any{"app_id": app.ID, "source_id": src.ID, "title": "Bitcoin", "value": "{{ btc.price | money }}"}, &sc)
	if len(sc.Checks) == 0 || !strings.Contains(strings.Join(sc.Checks, " "), "btc.usd") {
		t.Fatalf("a wrong path should come back as a check pointing at btc.usd: %+v", sc)
	}
	call("update_screen", map[string]any{"screen_id": sc.ID, "value": "{{ btc.usd | money }}", "sub": "{{ btc.change | fixed:1 | signed }}% today", "sub_color": "auto"}, &sc)
	if len(sc.Checks) != 0 || sc.Shows.Value != "$67.1k" || sc.Shows.Sub != "+2.3% today" {
		t.Fatalf("fixed screen: %+v", sc)
	}
	if res := call("add_screen", map[string]any{"title": "x"}, nil); !strings.Contains(errorText(res), "app_id") {
		t.Fatalf("add_screen without an app: %q", errorText(res))
	}

	// Installing reaches the board; someone else's published app can't be installed.
	var board struct {
		Apps []struct{ ID, Name string } `json:"installed_apps"`
	}
	call("install_app", map[string]any{"app_id": app.ID, "board_id": "AABBCCDDEEFF"}, &board)
	if len(board.Apps) != 1 || board.Apps[0].Name != "Crypto" {
		t.Fatalf("install_app: %+v", board)
	}
	state := nextState()
	if apps := state["apps"].([]any); len(apps) != 1 || apps[0].(map[string]any)["name"] != "Crypto" {
		t.Fatalf("the board didn't get the app: %v", state["apps"])
	}
	other, _ := st.CreateUser(ctx, "other@example.com", "x")
	theirs, _ := lib.CreateApp(ctx, other.ID, "Theirs", "", "")
	theirs.PublishedAt = time.Now().Unix()
	_ = st.SaveApp(ctx, theirs)
	if res := call("install_app", map[string]any{"app_id": theirs.ID, "board_id": "AABBCCDDEEFF"}, nil); !strings.Contains(errorText(res), "Only your own apps") {
		t.Fatalf("installing someone else's app: %q", errorText(res))
	}
	if res := call("get_app", map[string]any{"app_id": theirs.ID}, nil); !strings.Contains(errorText(res), "no such app") {
		t.Fatalf("someone else's app should be invisible: %q", errorText(res))
	}

	// Refresh tokens rotate; replaying an old one revokes the connection.
	var rt tokens
	form(http.DefaultClient, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tk.Refresh}, "client_id": {clientID}}, &rt)
	if rt.Access == "" || rt.Refresh == tk.Refresh {
		t.Fatalf("refresh: %+v", rt)
	}
	var replay tokens
	form(http.DefaultClient, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tk.Refresh}, "client_id": {clientID}}, &replay)
	if replay.Error != "invalid_grant" {
		t.Fatalf("a replayed refresh token should fail: %+v", replay)
	}
	if grants, _ := st.UserGrants(ctx, me.ID); len(grants) != 0 {
		t.Fatalf("the replay should revoke the grant, %d left", len(grants))
	}
	if _, err := connect(rt.Access); err == nil {
		t.Fatal("tokens of a revoked grant should stop working")
	}

	// Disconnect from the website.
	tk = exchange(approve(), verifier)
	var list struct {
		Grants []struct{ ID, ClientName string }
	}
	getJSON(browser, "/api/oauth/grants", &list)
	if len(list.Grants) != 1 || list.Grants[0].ClientName != "Claude" {
		t.Fatalf("grants: %+v", list)
	}
	form(browser, "/api/oauth/grants/"+list.Grants[0].ID+"/revoke", url.Values{}, nil)
	if _, err := connect(tk.Access); err == nil {
		t.Fatal("a disconnected client's token should stop working")
	}
}
