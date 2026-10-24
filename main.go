// Command app is makerApp: one Go binary that serves the website, the
// makerDevice WebSocket endpoint, the firmware for browser flashing, and the
// data-source poller. It runs on Sideyard (see AGENTS.md).
//
// Configuration (environment variables; Sideyard sets PORT, DATA_DIR,
// DATABASE_PATH and SIDEYARD_*):
//
//	PUBLIC_URL           public base URL, e.g. https://maker.app.rabeeta.xyz (derived from requests if unset)
//	SECRET_KEY           encrypts stored API tokens; required on Sideyard, generated and kept in the DB locally
//	ALLOW_SIGNUP         "false" to close registrations (default true)
//	ALLOW_PRIVATE_FETCH  "true" to let data sources call private/LAN addresses (default false)
//	LOG_LEVEL            "debug" for request logs
//	TZ                   time zone for the clock and @time (default the system's)
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"makerapp/internal/engine"
	"makerapp/internal/fetch"
	"makerapp/internal/fw"
	"makerapp/internal/library"
	"makerapp/internal/mcpserver"
	"makerapp/internal/secret"
	"makerapp/internal/store"
	"makerapp/internal/web"
)

//go:embed all:web/dist
var webDist embed.FS

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
	// Sideyard sends SIGTERM when a new release takes over.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Sideyard sets these. The fallbacks are for running locally.
	port := env("PORT", "3000")
	dataDir := env("DATA_DIR", "data")
	dbPath := env("DATABASE_PATH", filepath.Join(dataDir, "app.db"))

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	st := store.New(db)

	key, err := secretKey(ctx, st, log)
	if err != nil {
		return err
	}
	box, err := secret.New(key)
	if err != nil {
		return err
	}

	fc := fetch.New(envBool("ALLOW_PRIVATE_FETCH", false))
	eng := engine.New(st, box, fc, time.Local, log)
	go eng.Run(ctx)
	lib := library.New(st, eng)

	publicURL := os.Getenv("PUBLIC_URL")
	links := publicURL // where MCP answers link back to
	if links == "" {
		links = "http://localhost:" + port
	}
	site := web.New(web.Config{
		PublicURL:     publicURL,
		AllowSignup:   envBool("ALLOW_SIGNUP", true),
		SecureCookies: strings.HasPrefix(publicURL, "https://"),
		Frontend:      frontend(webDist),
		MCP:           mcpserver.Handler(mcpserver.Deps{Store: st, Engine: eng, Library: lib, Fetch: fc, PublicURL: links, Log: log}),
		Fetch:         fc,
	}, st, eng, lib, log)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(db))
	mux.HandleFunc("POST /_jobs/{name}", jobHandler(db))
	mux.Handle("/", site.Handler())

	// 127.0.0.1 only: Sideyard's proxy is the public entry point.
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() {
		log.Info("makerApp starting", "addr", "http://"+srv.Addr, "public_url", publicURL,
			"firmware", fw.Version(), "firmware_embedded", fw.Available())
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Boards reconnect as soon as their socket closes, so close them first: they
	// land on the new release instead of waiting for this one to exit.
	eng.CloseAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func healthz(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	}
}

// secretKey returns the key that encrypts the API tokens saved on sources. On
// Sideyard it must come from SECRET_KEY: a key kept in the database would
// unlock every token for anyone with a copy of the file, so a release without
// it fails to start (and its health check) instead of going live. Locally one
// is generated and kept in the database.
func secretKey(ctx context.Context, st *store.Store, log *slog.Logger) (string, error) {
	if key := os.Getenv("SECRET_KEY"); key != "" {
		return key, nil
	}
	if os.Getenv("SIDEYARD_TOKEN") != "" {
		return "", errors.New("SECRET_KEY is not set: add it in the project's Variables tab")
	}
	key, err := st.Meta(ctx, "secret_key")
	if errors.Is(err, store.ErrNotFound) {
		key = store.RandomToken(32)
		err = st.SetMeta(ctx, "secret_key", key)
		log.Warn("SECRET_KEY not set; generated one and stored it in the database")
	}
	return key, err
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}
