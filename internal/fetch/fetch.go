// Package fetch performs the outbound HTTP requests behind data sources and
// button webhooks. Private and loopback addresses are blocked unless allowed,
// since any signed-up user can make the server call a URL.
package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const maxBody = 2 << 20

type Client struct {
	http *http.Client
}

func New(allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
				ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
				return fmt.Errorf("blocked address %s (set ALLOW_PRIVATE_FETCH=1 to allow)", host)
			}
			return nil
		}
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        20,
		IdleConnTimeout:     60 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 25 * time.Second}}
}

// Param is an extra query parameter added at request time (used for secret
// API keys so they never sit in the visible URL).
type Param struct {
	K string `json:"k"`
	V string `json:"v"`
}

type Request struct {
	Method  string
	URL     string
	Query   []Param
	Auth    string // bare token becomes "Bearer <token>"; "Basic ..." etc. are sent as-is
	Headers string // one "Key: Value" per line
	Body    string
}

type Result struct {
	Status int
	Data   string // always valid JSON; non-JSON bodies become {"text": "..."}
}

func (c *Client) Do(ctx context.Context, r Request) (*Result, error) {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !strings.HasPrefix(r.URL, "http://") && !strings.HasPrefix(r.URL, "https://") {
		return nil, errors.New("URL must start with http:// or https://")
	}
	visibleURL := r.URL // without secret params, for error messages
	if len(r.Query) > 0 {
		u, err := url.Parse(r.URL)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		for _, p := range r.Query {
			if p.K != "" {
				q.Set(p.K, p.V)
			}
		}
		u.RawQuery = q.Encode()
		r.URL = u.String()
	}
	var body io.Reader
	if r.Body != "" && method != http.MethodGet {
		body = strings.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "makerApp/1.0")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a := strings.TrimSpace(r.Auth); a != "" {
		if !strings.Contains(a, " ") {
			a = "Bearer " + a
		}
		req.Header.Set("Authorization", a)
	}
	for _, line := range strings.Split(r.Headers, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) != "" {
			req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = visibleURL // never leak secret params into stored errors
		}
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	res := &Result{Status: resp.StatusCode, Data: ToJSON(raw)}
	if resp.StatusCode >= 400 {
		return res, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return res, nil
}

// PostJSON sends v as JSON and returns the response, used for button webhooks.
func (c *Client) PostJSON(ctx context.Context, url string, v any) (*Result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return c.Do(ctx, Request{Method: http.MethodPost, URL: url, Body: string(b)})
}

// ToJSON returns raw if it is JSON, otherwise wraps it as {"text": raw}.
func ToJSON(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && json.Valid(trimmed) {
		return string(trimmed)
	}
	b, _ := json.Marshal(map[string]string{"text": string(raw)})
	return string(b)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
