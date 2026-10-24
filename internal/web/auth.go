package web

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "makerapp_session"
	sessionTTL    = 30 * 24 * time.Hour
)

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if s.userFromCookie(r) != nil {
		http.Redirect(w, r, "/devices", http.StatusSeeOther)
		return
	}
	s.render(w, r, "landing", map[string]any{"AllowSignup": s.cfg.AllowSignup})
}

func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/devices"
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "auth", map[string]any{"Mode": "login", "Next": r.URL.Query().Get("next"), "AllowSignup": s.cfg.AllowSignup})
}

func (s *Server) signupPage(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowSignup {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, "auth", map[string]any{"Mode": "signup", "Next": r.URL.Query().Get("next"), "AllowSignup": true})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := s.st.CreateSession(r.Context(), userID, sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cfg.SecureCookies || r.TLS != nil, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	next := r.FormValue("next")
	fail := func(msg string) {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, r, "auth", map[string]any{"Mode": "login", "Error": msg, "Email": email, "Next": next,
			"AllowSignup": s.cfg.AllowSignup})
	}
	if !s.allow("login:"+clientIP(r), 10, time.Minute) {
		fail("Too many attempts. Wait a minute and try again.")
		return
	}
	u, err := s.st.UserByEmail(r.Context(), email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		fail("Wrong email or password.")
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		fail("Could not sign you in. Try again.")
		return
	}
	http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowSignup {
		http.Error(w, "signups are closed", http.StatusForbidden)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	next := r.FormValue("next")
	fail := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, "auth", map[string]any{"Mode": "signup", "Error": msg, "Email": email, "Next": next, "AllowSignup": true})
	}
	if _, err := mail.ParseAddress(email); err != nil {
		fail("Enter a valid email address.")
		return
	}
	if len(password) < 8 {
		fail("Use at least 8 characters for the password.")
		return
	}
	if !s.allow("signup:"+clientIP(r), 5, time.Hour) {
		fail("Too many sign-ups from this network. Try again later.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail("Could not create the account.")
		return
	}
	u, err := s.st.CreateUser(r.Context(), email, string(hash))
	if err != nil {
		fail("That email already has an account. Log in instead.")
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		fail("Account created, but signing in failed. Log in.")
		return
	}
	if next == "" {
		next = "/flash"
	}
	http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.st.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// clientIP is the address rate limits count against. Behind Sideyard's proxy
// that's the last X-Forwarded-For entry, the one the proxy added: entries
// before it come from the client and can be made up.
func clientIP(r *http.Request) string {
	if vs := r.Header.Values("X-Forwarded-For"); len(vs) > 0 {
		f := vs[len(vs)-1]
		return strings.TrimSpace(f[strings.LastIndex(f, ",")+1:])
	}
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i > 0 {
		ip = ip[:i]
	}
	return ip
}
