package web

// Sign-in for MCP clients (Claude, Codex, …): an OAuth 2.1 authorization server
// as the MCP spec describes it, kept in the app's own database. A client finds
// it through /mcp's 401, identifies itself with a metadata document URL or by
// registering, sends the user to the consent page, and trades the code (with
// its PKCE verifier) for tokens that work on /mcp only.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"makerapp/internal/fetch"
	"makerapp/internal/store"
)

const (
	oauthScope      = "apps"
	codeTTL         = 10 * time.Minute
	accessTTL       = time.Hour
	refreshTTL      = 60 * 24 * time.Hour
	clientCacheTime = 24 * time.Hour // how long a fetched client metadata document is trusted
)

// origin is the server's public origin for OAuth: PUBLIC_URL, or the address
// the request came to. Unlike publicBase it never swaps in the LAN IP: a
// client's URLs must match the address it connected to.
func (s *Server) origin(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// mcpResource is the MCP endpoint's URL: the one place makerApp's tokens work.
func (s *Server) mcpResource(r *http.Request) string { return s.origin(r) + "/mcp" }

// publicCORS lets browser-based MCP clients call the metadata and token
// endpoints; they use no cookies, so any origin is fine.
func publicCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// ---- discovery ----

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               s.mcpResource(r),
		AuthorizationServers:   []string{s.origin(r)},
		ScopesSupported:        []string{oauthScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "makerApp",
		ResourceDocumentation:  s.origin(r) + "/connect",
	}).ServeHTTP(w, r)
}

func (s *Server) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	o := s.origin(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         o,
		"authorization_endpoint":                         o + "/oauth/authorize",
		"token_endpoint":                                 o + "/oauth/token",
		"registration_endpoint":                          o + "/oauth/register",
		"scopes_supported":                               []string{oauthScope},
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	})
}

// ---- clients ----

// validRedirect accepts https URLs, http on the loopback address (native apps
// like Codex listen on a local port), and private-use schemes (cursor://…).
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Scheme == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopback(u.Hostname())
	case "javascript", "data", "file", "vbscript", "blob":
		return false
	}
	return true
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectAllowed matches a redirect URI against a client's list: exactly, or
// for loopback URIs on any port (RFC 8252: the port is picked at run time).
func redirectAllowed(allowed []string, got string) bool {
	if slices.Contains(allowed, got) {
		return true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" || !isLoopback(g.Hostname()) {
		return false
	}
	for _, a := range allowed {
		if u, err := url.Parse(a); err == nil && u.Scheme == "http" && u.Hostname() == g.Hostname() && u.Path == g.Path {
			return true
		}
	}
	return false
}

// register is Dynamic Client Registration (RFC 7591), for clients that don't
// use a metadata document. Every client is public: it proves itself with PKCE.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.lim.Allow("register:"+clientIP(r), 20, time.Hour) {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "Too many registrations from this address; try again later.")
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "The body must be JSON client metadata.")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Give between 1 and 10 redirect_uris.")
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirect(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Redirect URIs must be https, http on localhost, or an app's own scheme: "+u)
			return
		}
	}
	c := &store.OAuthClient{ID: store.RandomToken(16), Name: clientName(req.ClientName), RedirectURIs: req.RedirectURIs}
	if err := s.st.SaveOAuthClient(r.Context(), c); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Could not register the client.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func clientName(n string) string {
	n = strings.TrimSpace(n)
	if n == "" {
		return "An MCP client"
	}
	if r := []rune(n); len(r) > 60 {
		return string(r[:60])
	}
	return n
}

// client finds the client behind a client_id: a registered one, or one
// identified by the URL of its metadata document, fetched (through the same
// guard as data sources: no private addresses) and cached for a day.
func (s *Server) client(ctx context.Context, id string) (*store.OAuthClient, error) {
	if !strings.HasPrefix(id, "https://") {
		return s.st.OAuthClient(ctx, id)
	}
	if c, err := s.st.OAuthClient(ctx, id); err == nil && time.Since(time.Unix(c.FetchedAt, 0)) < clientCacheTime {
		return c, nil
	}
	if u, err := url.Parse(id); err != nil || u.Path == "" || u.Path == "/" {
		return nil, errors.New("A client_id URL must point at a metadata document.")
	}
	fctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := s.cfg.Fetch.Do(fctx, fetch.Request{Method: "GET", URL: id})
	if err != nil || res.Status != http.StatusOK || len(res.Data) > 16<<10 {
		return nil, errors.New("Couldn't read the client's metadata document.")
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if json.Unmarshal([]byte(res.Data), &doc) != nil || doc.ClientID != id || len(doc.RedirectURIs) == 0 {
		return nil, errors.New("The client's metadata document isn't valid.")
	}
	c := &store.OAuthClient{ID: id, Name: clientName(doc.ClientName), RedirectURIs: doc.RedirectURIs, FetchedAt: time.Now().Unix()}
	return c, s.st.SaveOAuthClient(ctx, c)
}

// ---- consent ----

type authRequest struct {
	client      *store.OAuthClient
	redirectURI string
	challenge   string
	state       string
	resource    string
}

// authRequestFrom checks an authorization request's parameters (the query the
// client sent the browser to /oauth/authorize with).
func (s *Server) authRequestFrom(r *http.Request) (*authRequest, error) {
	q := r.Form
	c, err := s.client(r.Context(), q.Get("client_id"))
	if errors.Is(err, store.ErrNotFound) {
		return nil, errors.New("This app isn't known to makerApp. Try connecting again from the app.")
	} else if err != nil {
		return nil, err
	}
	a := &authRequest{client: c, redirectURI: q.Get("redirect_uri"), challenge: q.Get("code_challenge"),
		state: q.Get("state"), resource: q.Get("resource")}
	switch {
	case !redirectAllowed(c.RedirectURIs, a.redirectURI):
		return nil, errors.New("The app asked to send you somewhere it didn't register. Not continuing.")
	case q.Get("response_type") != "code":
		return nil, errors.New("Unsupported response_type; makerApp supports \"code\".")
	case q.Get("code_challenge_method") != "S256" || len(a.challenge) < 43 || len(a.challenge) > 128:
		return nil, errors.New("The app must use PKCE with S256.")
	case a.resource != "" && strings.TrimRight(a.resource, "/") != s.mcpResource(r):
		return nil, errors.New("The app asked for access to a different server: " + a.resource)
	}
	a.resource = s.mcpResource(r)
	return a, nil
}

// authorizeInfo tells the consent page who's asking.
func (s *Server) authorizeInfo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	a, err := s.authRequestFrom(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	host := a.redirectURI
	if u, err := url.Parse(a.redirectURI); err == nil && u.Host != "" {
		host = u.Host
	}
	writeJSON(w, http.StatusOK, map[string]any{"Client": a.client.Name, "RedirectHost": host, "Email": currentUser(r).Email})
}

// authorize answers the consent page: Allow issues a one-time code, Cancel
// tells the client no. Either way the browser goes back to the client.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	a, err := s.authRequestFrom(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	back, _ := url.Parse(a.redirectURI)
	q := back.Query()
	if a.state != "" {
		q.Set("state", a.state)
	}
	q.Set("iss", s.origin(r))
	if r.Form.Get("decision") != "allow" {
		q.Set("error", "access_denied")
	} else {
		g, err := s.st.Grant(r.Context(), currentUser(r).ID, a.client.ID, a.client.Name)
		if err != nil {
			fail(w, http.StatusInternalServerError, "server error")
			return
		}
		code, err := s.st.CreateOAuthCode(r.Context(), store.OAuthCode{GrantID: g.ID, RedirectURI: a.redirectURI,
			Challenge: a.challenge, Resource: a.resource}, codeTTL)
		if err != nil {
			fail(w, http.StatusInternalServerError, "server error")
			return
		}
		q.Set("code", code)
	}
	back.RawQuery = q.Encode()
	redirect(w, back.String())
}

// ---- tokens ----

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if !s.lim.Allow("token:"+clientIP(r), 60, time.Minute) {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "Too many requests; slow down.")
		return
	}
	_ = r.ParseForm()
	f := r.PostForm
	if res := f.Get("resource"); res != "" && strings.TrimRight(res, "/") != s.mcpResource(r) {
		oauthError(w, http.StatusBadRequest, "invalid_target", "makerApp's tokens are only for "+s.mcpResource(r))
		return
	}
	var grant *store.OAuthGrant
	switch f.Get("grant_type") {
	case "authorization_code":
		c, g, err := s.st.UseOAuthCode(r.Context(), f.Get("code"))
		if err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The code is wrong, used or expired.")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		switch {
		case g.ClientID != f.Get("client_id"):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The code was issued to another client.")
			return
		case c.RedirectURI != f.Get("redirect_uri"):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri doesn't match the authorization request.")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != c.Challenge:
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The PKCE code_verifier doesn't match.")
			return
		}
		grant = g
	case "refresh_token":
		g, err := s.st.UseRefreshToken(r.Context(), f.Get("refresh_token"))
		if errors.Is(err, store.ErrTokenReused) {
			s.log.Warn("refresh token reused; grant revoked", "client", f.Get("client_id"))
		}
		if err != nil || g.ClientID != f.Get("client_id") {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token is wrong, used or expired. Connect again.")
			return
		}
		grant = g
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "Use authorization_code or refresh_token.")
		return
	}
	access, refresh, err := s.st.IssueOAuthTokens(r.Context(), grant.ID, accessTTL, refreshTTL)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Could not issue tokens.")
		return
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         oauthScope,
	})
}

// verifyMCPToken checks a bearer token on /mcp and says whose it is.
func (s *Server) verifyMCPToken(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	g, exp, err := s.st.OAuthAccess(ctx, token)
	if err != nil {
		return nil, auth.ErrInvalidToken
	}
	_ = s.st.TouchGrant(ctx, g.ID)
	return &auth.TokenInfo{UserID: g.UserID, Expiration: time.Unix(exp, 0), Scopes: []string{oauthScope},
		Extra: map[string]any{"client": g.ClientName}}, nil
}

// mcp serves the MCP endpoint to clients with a valid token. Without one the
// 401 points at the resource metadata, which starts the sign-in.
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	auth.RequireBearerToken(s.verifyMCPToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.origin(r) + "/.well-known/oauth-protected-resource/mcp",
		Scopes:              []string{oauthScope},
	})(s.cfg.MCP).ServeHTTP(w, r)
}

// ---- connected AIs ----

func (s *Server) grants(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.UserGrants(r.Context(), currentUser(r).ID)
	if notFound(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Grants": list, "URL": s.mcpResource(r)})
}

func (s *Server) revokeGrant(w http.ResponseWriter, r *http.Request) {
	if err := s.st.RevokeGrant(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		fail(w, http.StatusInternalServerError, "server error")
		return
	}
	redirect(w, "/connect")
}
