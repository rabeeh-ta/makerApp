package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"net/http"
	"os"
)

// jobs are run by Sideyard on the schedules set in the dashboard's Jobs tab:
// it calls POST /_jobs/<name> with the app's SIDEYARD_TOKEN. To add one,
// write a function here, deploy, then create the job in the dashboard.
// A returned error fails the run; the returned text is shown as its output.
//
// The data-source poller isn't a job: sources refresh as often as every 15
// seconds, and jobs run at most once a minute (see AGENTS.md).
var jobs = map[string]func(ctx context.Context, db *sql.DB) (string, error){}

func jobHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("SIDEYARD_TOKEN")
		got := r.Header.Get("Authorization")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		job, ok := jobs[r.PathValue("name")]
		if !ok {
			http.Error(w, "no such job", http.StatusNotFound)
			return
		}
		out, err := job(r.Context(), db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, out)
	}
}
