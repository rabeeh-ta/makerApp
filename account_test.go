package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/library"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

// TestAccount covers the profile page and changing the password: the old
// password stops working and other browsers are signed out, this one isn't.
func TestAccount(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := store.New(db)
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, box, fetch.New(true), time.UTC, log)
	srv := httptest.NewServer(web.New(web.Config{AllowSignup: true}, st, eng, library.New(st, eng), log).Handler())
	defer srv.Close()

	browser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar}
	}
	// call sends a form (GET when form is nil) and decodes the JSON reply.
	call := func(c *http.Client, path string, form url.Values, out any) int {
		t.Helper()
		var resp *http.Response
		var err error
		if form == nil {
			resp, err = c.Get(srv.URL + path)
		} else {
			resp, err = c.PostForm(srv.URL+path, form)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}

	laptop, phone := browser(), browser()
	if code := call(laptop, "/api/signup", url.Values{"email": {"me@example.com"}, "password": {"password123"}}, nil); code != 200 {
		t.Fatalf("signup: %d", code)
	}
	if code := call(phone, "/api/login", url.Values{"email": {"me@example.com"}, "password": {"password123"}}, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}

	var acct struct {
		Email, Username       string
		Devices, Apps, AIApps int
	}
	if code := call(laptop, "/api/account", nil, &acct); code != 200 || acct.Email != "me@example.com" || acct.Devices != 0 {
		t.Fatalf("account: %d %+v", code, acct)
	}

	change := func(current, password, confirm string) (int, string) {
		var r struct{ Redirect, Error string }
		code := call(laptop, "/api/account/password", url.Values{"current": {current}, "password": {password}, "confirm": {confirm}}, &r)
		return code, r.Error + r.Redirect
	}
	// Mistakes answer 400 (never 401, which the website reads as signed out).
	for _, c := range [][3]string{{"wrong-pass", "newpass123", "newpass123"}, {"password123", "short", "short"}, {"password123", "newpass123", "newpass124"}} {
		if code, msg := change(c[0], c[1], c[2]); code != http.StatusBadRequest {
			t.Fatalf("change %v: %d %q", c, code, msg)
		}
	}
	if code, to := change("password123", "newpass123", "newpass123"); code != 200 || to != "/account?changed" {
		t.Fatalf("change: %d %q", code, to)
	}

	if code := call(laptop, "/api/account", nil, nil); code != 200 {
		t.Fatalf("this browser after change: %d, want 200", code)
	}
	if code := call(phone, "/api/account", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("other browser after change: %d, want 401", code)
	}
	if code := call(browser(), "/api/login", url.Values{"email": {"me@example.com"}, "password": {"password123"}}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old password: %d, want 401", code)
	}
	if code := call(browser(), "/api/login", url.Values{"email": {"me@example.com"}, "password": {"newpass123"}}, nil); code != 200 {
		t.Fatalf("new password: %d, want 200", code)
	}
}
