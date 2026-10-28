package main

import (
	"database/sql"
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

// testSite is the server on a fresh database, for tests that use its API the
// way the website does.
type testSite struct {
	t   *testing.T
	url string
	db  *sql.DB
	st  *store.Store
	eng *engine.Engine
}

func newTestSite(t *testing.T) *testSite {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := store.New(db)
	box, _ := secret.New("test")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, box, fetch.New(true), time.UTC, log)
	srv := httptest.NewServer(web.New(web.Config{AllowSignup: true}, st, eng, library.New(st, eng), log).Handler())
	t.Cleanup(srv.Close)
	return &testSite{t: t, url: srv.URL, db: db, st: st, eng: eng}
}

// browser is a new browser, signed out, with its own cookies.
func (s *testSite) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// call sends a form (GET when form is nil) and decodes the JSON reply into out.
func (s *testSite) call(c *http.Client, path string, form url.Values, out any) int {
	s.t.Helper()
	var resp *http.Response
	var err error
	if form == nil {
		resp, err = c.Get(s.url + path)
	} else {
		resp, err = c.PostForm(s.url+path, form)
	}
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// post expects a form post that worked, and returns the page it sends the website to.
func (s *testSite) post(c *http.Client, path string, form url.Values) string {
	s.t.Helper()
	var r struct{ Redirect, Error string }
	if code := s.call(c, path, form, &r); code != 200 || r.Error != "" {
		s.t.Fatalf("POST %s: %d %q", path, code, r.Error)
	}
	return r.Redirect
}

// signUp is a browser signed in to a new account.
func (s *testSite) signUp(email string) *http.Client {
	s.t.Helper()
	c := s.browser()
	s.post(c, "/api/signup", url.Values{"email": {email}, "password": {"password123"}})
	return c
}
