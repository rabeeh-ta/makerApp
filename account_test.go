package main

import (
	"net/http"
	"net/url"
	"testing"
)

// TestAccount covers the profile page and changing the password: the old
// password stops working and other browsers are signed out, this one isn't.
func TestAccount(t *testing.T) {
	site := newTestSite(t)
	laptop := site.signUp("me@example.com")
	phone := site.browser()
	site.post(phone, "/api/login", url.Values{"email": {"me@example.com"}, "password": {"password123"}})

	var acct struct {
		Email, Username       string
		Devices, Apps, AIApps int
	}
	if code := site.call(laptop, "/api/account", nil, &acct); code != 200 || acct.Email != "me@example.com" || acct.Devices != 0 {
		t.Fatalf("account: %d %+v", code, acct)
	}

	change := func(current, password, confirm string) (int, string) {
		var r struct{ Redirect, Error string }
		code := site.call(laptop, "/api/account/password", url.Values{"current": {current}, "password": {password}, "confirm": {confirm}}, &r)
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

	if code := site.call(laptop, "/api/account", nil, nil); code != 200 {
		t.Fatalf("this browser after change: %d, want 200", code)
	}
	if code := site.call(phone, "/api/account", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("other browser after change: %d, want 401", code)
	}
	if code := site.call(site.browser(), "/api/login", url.Values{"email": {"me@example.com"}, "password": {"password123"}}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old password: %d, want 401", code)
	}
	if code := site.call(site.browser(), "/api/login", url.Values{"email": {"me@example.com"}, "password": {"newpass123"}}, nil); code != 200 {
		t.Fatalf("new password: %d, want 200", code)
	}
}
