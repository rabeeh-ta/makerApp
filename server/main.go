// espHog server: one binary that serves the website, the device WebSocket
// endpoint, the firmware for browser flashing, and the data-source poller.
//
// Configuration (environment variables):
//
//	ADDR                 listen address (default :8080, or 127.0.0.1:$PORT when PORT is set)
//	PORT                 set by Sideyard; the app then listens on loopback behind Sideyard's proxy
//	DATABASE_URL         SQLite file path (default data/esphog.db) or libsql://...?authToken=... for Turso
//	PUBLIC_URL           public base URL, e.g. https://maker.rabeeta.xyz (derived from requests if unset)
//	SECRET_KEY           encrypts stored API tokens (generated and stored in the DB if unset)
//	ALLOW_SIGNUP         "false" to close registrations (default true)
//	ALLOW_PRIVATE_FETCH  "true" to let data sources call private/LAN addresses (default false)
//	AUTOCERT_DOMAIN      serve HTTPS on :443 with Let's Encrypt for this domain (optional)
//	CERT_DIR             where Let's Encrypt certificates are cached (default data/certs)
//	LOG_LEVEL            "debug" for request logs
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"esphog/internal/engine"
	"esphog/internal/fetch"
	"esphog/internal/fw"
	"esphog/internal/secret"
	"esphog/internal/store"
	"esphog/internal/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func main() {
	level := slog.LevelInfo
	if env("LOG_LEVEL", "") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := env("DATABASE_URL", "data/esphog.db")
	if !strings.Contains(dsn, "://") {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return err
		}
	}
	st, err := store.Open(dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	key := os.Getenv("SECRET_KEY")
	if key == "" {
		key, err = st.Meta(ctx, "secret_key")
		if errors.Is(err, store.ErrNotFound) {
			key = store.RandomToken(32)
			err = st.SetMeta(ctx, "secret_key", key)
			log.Warn("SECRET_KEY not set; generated one and stored it in the database")
		}
		if err != nil {
			return err
		}
	}
	box, err := secret.New(key)
	if err != nil {
		return err
	}

	eng := engine.New(st, box, fetch.New(envBool("ALLOW_PRIVATE_FETCH", false)), time.Local, log)
	go eng.Run(ctx)

	domain := os.Getenv("AUTOCERT_DOMAIN")
	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" && domain != "" {
		publicURL = "https://" + domain
	}
	site, err := web.New(web.Config{
		PublicURL:     publicURL,
		AllowSignup:   envBool("ALLOW_SIGNUP", true),
		SecureCookies: strings.HasPrefix(publicURL, "https://"),
	}, st, eng, log)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           site.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	log.Info("espHog starting", "firmware", fw.Version(), "firmware_embedded", fw.Available(), "db", redact(dsn))

	if domain != "" {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(domain),
			Cache:      autocert.DirCache(env("CERT_DIR", "data/certs")),
		}
		go func() {
			// :80 answers ACME challenges and redirects everything else to HTTPS.
			if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
				log.Error("http :80", "err", err)
			}
		}()
		srv.Addr = ":443"
		srv.TLSConfig = m.TLSConfig()
		log.Info("listening", "addr", "https://"+domain)
		err = srv.ListenAndServeTLS("", "")
	} else {
		srv.Addr = listenAddr()
		log.Info("listening", "addr", srv.Addr, "public_url", publicURL)
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func listenAddr() string {
	if addr := os.Getenv("ADDR"); addr != "" {
		return addr
	}
	if port := os.Getenv("PORT"); port != "" {
		return "127.0.0.1:" + port
	}
	return ":8080"
}

func redact(dsn string) string {
	if i := strings.Index(dsn, "authToken="); i >= 0 {
		return dsn[:i] + "authToken=***"
	}
	return dsn
}
