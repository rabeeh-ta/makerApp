# Moving makerApp onto Sideyard

Status: **Phase 2 in progress on branch `sideyard` (2026-10-24).** Steps 1 and 2 are done
(section 8, Progress); the old template UI still runs on the new store. Next: step 3, the
JSON API. `SECRET_KEY` is set in the `maker` project's Variables tab.

**Name (decided 2026-10-24):** the platform is **makerApp** and the board is
**makerDevice**. espHog was the first name and is no longer used, so Phase 2 renames it
everywhere (D6). The firmware's screens, hotspot and installer already say makerDevice.

**Fresh start (decided 2026-10-23):** no existing data, deployment or boards are carried
over. The new app starts with an empty database, and boards are flashed from the new
site. The laptop's `data/esphog.db` stays as it is and isn't migrated. There's no data copy
(phase 3) and no old host to cut over from.

**Domain: `maker.app.rabeeta.xyz`** (`PUBLIC_URL=https://maker.app.rabeeta.xyz`). It
already resolves to `3.85.79.112`. HTTPS there fails for now (TLS internal error), which is
expected until the project and domain exist in Sideyard. `maker.rabeeta.xyz` is no longer
used.

Audited on 2026-10-23 at `master` (`e074348`). Baseline: `go vet ./...` is clean and
`go test ./...` passes in `server/`.

---

## 1. Fit

**It fits one Sideyard app.** espHog is already one Go process with one SQLite file. It
has no other services, no cron, no file storage and no email. Two pieces of work are needed:

- the server-rendered UI (Go `html/template` + htmx) becomes a Vite + React + TypeScript
  single-page app, and
- the store's custom migration code is replaced by the `migrations` list in `db.go`, using
  the type rules from the brief.

**What moves:** this is a small monorepo. `server/` becomes the Sideyard app at the repo
root. `firmware/` (ESP32 C++, PlatformIO) stays as a sibling directory. It isn't a server:
it's built on a laptop and its output (`server/internal/fw/bin/*.bin`, committed) is
embedded in the binary. `backup/` is an untracked CircuitPython backup and isn't part of
anything.

**Load.** Memory per board is one WebSocket with three goroutines and a 32-message send
queue (at most 15 KB per message). The poller runs at most 8 fetches at a time, with
responses capped at 2 MB each. Writes are about one per board every 25 s (`TouchDevice` on
each ping) plus one per source fetch. A few hundred boards is a few writes a second and
well under 512 MB. Nothing needs another server, region or language.

**What doesn't map 1:1** (details in section 3):

1. **The data-source poller** is a goroutine on a 5-second tick. It refreshes each source
   on its own interval, which can be as short as 15 s. Sideyard jobs are schedules, so they
   can't express that. I propose keeping the poller as an in-process worker over a table:
   the brief's mapping for queues and workers. That conflicts with the AGENTS.md line
   "scheduled work runs through Sideyard, not goroutine timers", so it's decision **D2**.
2. **Flashed boards hard-code the server URL** (`wss://<host>/ws`, written by the browser
   flasher from `PUBLIC_URL`). The device-facing routes have to stay byte-for-byte the
   same. Once real boards are flashed, the domain can't change without re-flashing them.
   The firmware's built-in fallback still says `maker.rabeeta.xyz` (D8).

**Nothing is live today.** `maker.rabeeta.xyz` has no DNS record, and the only database is
the laptop's development copy. It isn't migrated (fresh start, above). The new
deployment goes to `maker.app.rabeeta.xyz`.

---

## 2. Inventory

| Area | Today |
|---|---|
| Backend | Go 1.26, `net/http` `ServeMux` with method patterns, no framework. Module `esphog` in `server/`, about 5,000 lines: `engine` (WebSocket hub, poller, device state), `store` (SQL), `render` (screen templating with gjson), `fetch` (outbound HTTP with an SSRF guard), `fw` (embedded firmware, ESP Web Tools manifest, config image), `games` (embedded Lua games), `secret` (AES-GCM), `web` (pages and handlers, about 2,060 lines). |
| Frontend | Server-rendered `html/template`: `layout.html`, 12 pages, 4 partial files. Uses htmx 2.0.11 for polling and partial swaps, `app.js` (53 lines: `{{ path }}` insertion and show/hide by layout) and `data.js` (235 lines: JSON tree explorer, preset cards, response tabs). One stylesheet, `style.css` (553 lines). ESP Web Tools is vendored in `static/ewt/` (26 files, 532 KB, Web Serial flasher web component). Inter and JetBrains Mono load from Google Fonts. No Open Graph tags, `robots.txt` or sitemap. |
| Database | SQLite through `modernc.org/sqlite` (`DATABASE_URL`, default `data/esphog.db`), or Turso through `libsql-client-go` when `DATABASE_URL` is `libsql://…`. WAL mode, `foreign_keys(1)`, one open connection. 12 tables (section 5). The schema is applied on each start with `CREATE … IF NOT EXISTS`, a column-add list and a one-time `migrateToApps` (guarded by `meta.apps_migrated`). `PRAGMA user_version` is 0. |
| Data size | **Laptop DB:** 144 KB. users 1, sessions 4, claims 27, devices 1, sources 1, screens 0, apps 0, device_apps 3, app_installs 3, flows 1, device_screens 0, meta 2. Not migrated. Size is driven by `sources.last_data` (the latest JSON per source, up to 2 MB each). |
| Caches | None external. In-process: the login/signup/pair rate limiter (map + mutex), connected boards (`engine.conns`), in-flight fetches, and a per-request source cache. |
| Queues / workers | `engine.Run`: a 5 s ticker selects due HTTP sources (`last_fetched_at + interval_sec <= now`) and fetches up to 8 at a time. Changed data is pushed to boards. Each board connection has read, write and ping (25 s) goroutines. |
| Scheduled tasks | None. There's no cron; expired sessions and claims are never deleted. |
| File uploads | None. Firmware binaries and Lua games are build-time assets embedded with `go:embed`. |
| Email | None. There's no password reset. |
| Auth & sessions | Email + bcrypt (`$2a$`) passwords. `sessions` stores the SHA-256 of a 32-byte token. The cookie `esphog_session` is `HttpOnly`, `SameSite=Lax`, `Secure` when `PUBLIC_URL` is https, and lasts 30 days. CSRF protection is an `Origin`-host check on POSTs (`/push/` exempt). Rate limits: login 10/min/IP, signup 5/h/IP, pairing 10/min/user. Boards authenticate by trust-on-first-use secret (`devices.token_hash`), flash claim tokens (`claims`, 24 h) and 6-digit pair codes. Public usernames are unique and set once. Push sources use unguessable tokens. |
| WebSockets | `GET /ws` (`coder/websocket`, subprotocols `esphog` and `arduino`, origin check off because boards authenticate in `hello`, 64 KB read limit, state messages capped at 15 KB). Protocol is described in the README. |
| Env vars | `PORT`, `ADDR`, `DATABASE_URL`, `PUBLIC_URL`, `SECRET_KEY` (if unset, generated and stored in `meta.secret_key`), `ALLOW_SIGNUP`, `ALLOW_PRIVATE_FETCH`, `AUTOCERT_DOMAIN`, `CERT_DIR`, `LOG_LEVEL`, `TZ` (Go's `time.Local`, used for the clock's offset and `@time`). |
| Third-party APIs | Only what users configure as sources (any URL; presets for Meteosource, Open-Meteo, PostHog, HubSpot, CoinGecko, GitHub). Their tokens are stored encrypted per source. The app has no API keys of its own. The browser loads Google Fonts. |
| Build & deploy today | Root `Makefile`: `make firmware` (PlatformIO, copies `.bin` into `server/internal/fw/bin`, committed), `make server`, `make linux`, `make deploy` (builds `bin/esphog-sideyard` and curls it to Sideyard's deploy API with `.env.deploy`), `make test`, `make games-check` (compiles Lua and the game VM in C++). README alternatives: systemd + built-in Let's Encrypt, or Caddy. There's no Dockerfile and no CI. GitHub remote is `rabeeh-ta/makerApp`, default branch **`master`**. |

---

## 3. Mapping

| Today | On Sideyard | Notes |
|---|---|---|
| Go `net/http` server in `server/` | Same Go code at the repo root: `main.go` (starter) wires `engine` and the routes, `internal/*` packages move up unchanged | Module name stays `esphog` (D6); binary is `app`. |
| Server-rendered pages + htmx | React + TS SPA in `web/`, embedded from `web/dist`. Each page keeps its URL as a client route and gets its data from a `GET /api/…` JSON endpoint. Each form POST becomes a `POST /api/…` that returns JSON (or a validation error) instead of a 303. | `style.css` moves over as is, so the look doesn't change. Routing is a ~50-line history-API router in `web/src`, with no router dependency. |
| htmx polling (device badges and cards every 5 s, flash and pair status every 2 s) | `setInterval` + `fetch` in React at the same intervals | Polling is fine here; no SSE needed. |
| htmx live preview (`POST /screens/preview` returns LCD HTML) | `POST /api/screens/preview` returns `render.Screen` JSON, drawn by a `<DeviceScreen>` component (port of `partials/device_screen.html` + `app_art.html`) | |
| `data.js` JSON explorer, `app.js` path insertion | React components (`JsonTree`, field insertion at the cursor) | Highest-risk UI port (section 8). |
| ESP Web Tools at `/static/ewt/` | Same files in `web/public/static/ewt/`, so they keep the URL `/static/ewt/…`. `<esp-web-install-button>` is used as a custom element in React 19. | |
| Pages that must work before JavaScript runs | None. No OG tags, robots or sitemap today. `index.html` keeps the `makerApp` title; pages set `document.title`. | |
| Redirects: `GET /` → `/devices` when logged in; `GET /screens` → `/apps`; unauthenticated page → `/login?next=` | `GET /screens` stays a Go 303 registered before the frontend. The other two happen in the browser: the SPA asks `GET /api/me` and navigates. | The API answers 401 JSON instead of redirecting (section 6). |
| Device-facing routes (`/ws`, `/fw/…`, `/flash/manifest.json`, `/flash/config.bin`, `/flash/{part}`, `/flash/{hash}/{part}`, `/games/…`, `/push/{token}`) | Unchanged Go handlers, registered before the frontend | A hard contract with flashed boards and users' scripts. |
| SQLite file or Turso (`DATABASE_URL`) | SQLite at `$DATABASE_PATH` via the starter's `openDB`. Schema in `db.go` `migrations` (section 5). | The libsql/Turso driver and its dependencies are removed. |
| Custom migrator (`CREATE IF NOT EXISTS`, column list, `migrateToApps`) | `migrations` + `PRAGMA user_version` from `db.go` | Legacy tables and the migrator are dropped (5.4). |
| Plain SQL in `store` | Stays plain SQL through `database/sql`. `store.Open(dsn)` becomes `store.New(db *sql.DB)`, taking the handle from `openDB`. | |
| Poller goroutine (5 s tick, per-source intervals ≥ 15 s) | **No clean equivalent.** Closest fit is the brief's queue/worker row: `sources` is the task table (due when `last_fetched_at + interval_sec` has passed) and `engine.Run` is the goroutine started in `main`, stopped on SIGTERM. | A Sideyard job runs at most every minute, which would drop the 15 s and 30 s intervals. (D2) |
| Cron | None. `jobs.go` is added with an empty `jobs` map (the starter's `prune-notes` is removed). | Optional later: a daily job that deletes expired sessions and claims. Not planned, since today's app doesn't do it. |
| In-memory rate limiter | Stays in memory | Resets on each deploy, as it does today. |
| Sessions in SQLite + cookie | Unchanged: `sessions` table, `HttpOnly` cookie, `Secure` behind HTTPS | |
| `users` with bcrypt hashes | `users` table, bcrypt as today | Fresh start: everyone signs up again. |
| Device WebSocket hub | Kept, served by the Go server behind Sideyard's proxy | Plus: on SIGTERM, close board sockets with "going away" so boards reconnect to the new release at once, not when the old process exits (up to 8 s later with the starter's shutdown). |
| Uploads / S3 | None. Nothing for the Storage tab. | |
| Email | None. `email.go` is added for the contract and is unused. | |
| `.env`, `.env.deploy`, env vars | Variables tab: `PUBLIC_URL`, `SECRET_KEY` (D4), `ALLOW_SIGNUP`, `ALLOW_PRIVATE_FETCH`, `LOG_LEVEL`, `TZ`. `.env.deploy` stays for `make deploy` from a laptop. | Removed: `ADDR`, `DATABASE_URL`, `AUTOCERT_DOMAIN`, `CERT_DIR`. Sideyard sets `PORT`, `DATA_DIR`, `DATABASE_PATH` and `SIDEYARD_*`. |
| Built-in Let's Encrypt (`autocert`), systemd/Caddy instructions | Removed. Sideyard terminates HTTPS. | README rewritten. |
| `make deploy` curling Sideyard | Starter `Makefile` + `scripts/deploy.sh` + `.github/workflows/deploy.yml`. Kept as extra targets: `firmware`, `games-check`, `test`. | CI uses the committed firmware `.bin`s, so it doesn't need PlatformIO (same as today). |
| Graceful shutdown | Starter `main.go` (in-flight requests finish, exit within 10 s) | Today's `run()` returns as soon as `ListenAndServe` returns, without waiting for `Shutdown`. The starter fixes this. |
| `/healthz` returns `ok` without checks | Starter's `/healthz`, which pings the database | |

---

## 4. Routes checklist

Key: **D** = device-facing, ported unchanged. **P** = page that becomes a client route of the
SPA plus a JSON endpoint. **A** = action that becomes a JSON endpoint. Every `/api` route
keeps the old auth check (`auth` middleware → 401 JSON), the old validation messages and
the old status codes (400 form errors, 401 bad login, 403 signups closed or cross-site,
404 not found, 500).

### Sideyard and static
- [ ] `GET /healthz` → starter version (pings the DB)
- [ ] `POST /_jobs/{name}` → starter `jobHandler` (no jobs yet)
- [ ] `GET /static/…` → only `static/ewt/*` remains, served from `web/dist`; `style.css`, `app.js`, `data.js` and `htmx.min.js` are deleted once replaced
- [ ] `GET /` (catch-all) → `frontend(webDist)`

### Device-facing (D, same path, method, headers and bytes)
- [ ] `GET /ws`
- [ ] `GET /fw/firmware.bin`
- [ ] `GET /fw/{hash}/firmware.bin`
- [ ] `GET /flash/manifest.json?claim=`
- [ ] `GET /flash/config.bin?claim=`
- [ ] `GET /flash/{part}`
- [ ] `GET /flash/{hash}/{part}`
- [ ] `GET /games/{id}/{file}`
- [ ] `POST /push/{token}` (JSON in, `{"ok":true}` / `{"error":…}` out, same codes)

### Accounts
- [ ] `GET /{$}` (P) landing, or → `/devices` when logged in; uses `GET /api/me`
- [ ] `GET /login` (P)
- [ ] `POST /login` (A) → `POST /api/login` (401 + message; rate limited)
- [ ] `GET /signup` (P) → `/login` when signups are closed (`allowSignup` from `GET /api/me`)
- [ ] `POST /signup` (A) → `POST /api/signup` (400 / 403)
- [ ] `POST /logout` (A) → `POST /api/logout`
- [ ] new: `GET /api/me` → `{user: {email, username} | null, allowSignup}`

### Flashing and pairing
- [ ] `GET /flash` (P) board picker; `?board=` creates a claim → `POST /api/flash/claims` (create on POST, not GET)
- [ ] `GET /flash/status?claim=` (A, polled 2 s) → `GET /api/flash/status?claim=`
- [ ] `GET /pair?device=&code=` (P; this URL is printed on the board's screen) → `POST /api/pair/try`
- [ ] `POST /pair` (A, code form) → `POST /api/pair`
- [ ] `GET /pair/status` (A, polled 2 s) → `GET /api/pair/status`

### Devices
- [ ] `GET /devices` (P) → `GET /api/devices` (cards + getting-started flags)
- [ ] `GET /devices/list` (A, polled 5 s) → same `GET /api/devices`
- [ ] `GET /devices/{id}` (P) → `GET /api/devices/{id}` (installed apps, choices, state size)
- [ ] `GET /devices/{id}/status` (A, polled 5 s) → `GET /api/devices/{id}/status`
- [ ] `GET /devices/{id}/settings` (P) → `GET /api/devices/{id}`
- [ ] `POST /devices/{id}` (A, settings) → `POST /api/devices/{id}`
- [ ] `POST /devices/{id}/do/{action}` (A: identify, toast, sync, update, reboot, forget) → `POST /api/devices/{id}/do/{action}`
- [ ] `POST /devices/{id}/apps/{app}/move` (A) → `POST /api/devices/{id}/apps/{app}/move`

### Data sources
- [ ] `GET /sources` (P) → `GET /api/sources`
- [ ] `GET /sources/new` (P) → presets from `GET /api/sources/presets`
- [ ] `POST /sources/new` (A) → `POST /api/sources`
- [ ] `GET /sources/{id}` (P) → `GET /api/sources/{id}` (same exposure as today: header values, secret param *names*, `hasAuth`, never the token)
- [ ] `POST /sources/{id}` (A) → `POST /api/sources/{id}`
- [ ] `POST /sources/{id}/fetch` (A) → `POST /api/sources/{id}/fetch`
- [ ] `POST /sources/{id}/delete` (A) → `POST /api/sources/{id}/delete`

### Apps
- [ ] `GET /apps` (P) → `GET /api/apps` (yours + public list)
- [ ] `POST /apps/new` (A) → `POST /api/apps`
- [ ] `GET /apps/{id}` (P) → `GET /api/apps/{id}`
- [ ] `POST /apps/{id}` (A) → `POST /api/apps/{id}`
- [ ] `POST /apps/{id}/delete` (A) → `POST /api/apps/{id}/delete`
- [ ] `POST /apps/{id}/install` (A) → `POST /api/apps/{id}/install`
- [ ] `POST /apps/{id}/uninstall` (A) → `POST /api/apps/{id}/uninstall`
- [ ] `POST /apps/{id}/publish` (A) → `POST /api/apps/{id}/publish`
- [ ] `POST /apps/{id}/unpublish` (A) → `POST /api/apps/{id}/unpublish`
- [ ] `POST /apps/{id}/screens/{sid}/move` (A) → `POST /api/apps/{id}/screens/{sid}/move`

### Screens
- [ ] `GET /screens` → Go 303 to `/apps` (kept)
- [ ] `GET /screens/new` (P) → `GET /api/screens/new?app=&source=` (defaults)
- [ ] `POST /screens/new` (A) → `POST /api/screens`
- [ ] `POST /screens/preview` (A) → `POST /api/screens/preview` (JSON `render.Screen`)
- [ ] `GET /screens/explorer?source_id=` (A) → `GET /api/sources/{id}/paths`
- [ ] `GET /screens/{id}` (P) → `GET /api/screens/{id}`
- [ ] `POST /screens/{id}` (A) → `POST /api/screens/{id}`
- [ ] `POST /screens/{id}/delete` (A) → `POST /api/screens/{id}/delete`

The API keeps POST for all writes, matching today's forms. The `Origin` check covers every
POST except `/push/` (boards and scripts) and `/_jobs/` (Sideyard sends no `Origin`).

---

## 5. Data plan

### 5.1 Source

None. The fresh start means no rows are copied. This section is the schema the new app
starts with.

### 5.2 Target schema (first entries of `migrations` in `db.go`, one statement each)

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,            -- bcrypt
  username TEXT,                          -- public name, NULL until chosen
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_users_username ON users(username) WHERE username IS NOT NULL;

CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE claims (
  token TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  device_id TEXT                          -- set once a board uses it
);

CREATE TABLE devices (
  id TEXT PRIMARY KEY,                    -- 12 hex chars from the board
  user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  name TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL,
  pair_code TEXT NOT NULL DEFAULT '',
  fw_version TEXT NOT NULL DEFAULT '',
  board TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  rssi INTEGER NOT NULL DEFAULT 0,
  brightness INTEGER NOT NULL DEFAULT 100,
  flipped INTEGER NOT NULL DEFAULT 0 CHECK (flipped IN (0, 1)),
  offset_x INTEGER NOT NULL DEFAULT 0,
  offset_y INTEGER NOT NULL DEFAULT 0,
  last_seen TEXT,                         -- NULL: never seen (was 0)
  created_at TEXT NOT NULL
);

CREATE TABLE sources (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('http', 'static', 'push')),
  method TEXT NOT NULL DEFAULT 'GET',
  url TEXT NOT NULL DEFAULT '',
  secret TEXT NOT NULL DEFAULT '',        -- AES-GCM sealed JSON, base64
  body TEXT NOT NULL DEFAULT '',
  interval_sec INTEGER NOT NULL DEFAULT 300,
  push_token TEXT NOT NULL DEFAULT '',
  last_data TEXT NOT NULL DEFAULT '',     -- JSON
  last_status INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  last_fetched_at TEXT,                   -- NULL: never fetched (was 0)
  created_at TEXT NOT NULL
);
CREATE INDEX idx_sources_user ON sources(user_id);
CREATE UNIQUE INDEX idx_sources_push ON sources(push_token) WHERE push_token <> '';

CREATE TABLE apps (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  cycle_sec INTEGER NOT NULL DEFAULT 0,
  published_at TEXT,                      -- NULL: not published (was 0)
  updated_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_apps_user ON apps(user_id);

CREATE TABLE screens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  app_id TEXT,                            -- no FK, as today; DeleteApp removes screens itself
  app_pos INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL,
  source_id TEXT REFERENCES sources(id) ON DELETE SET NULL,
  layout TEXT NOT NULL CHECK (layout IN ('big_number', 'list', 'text', 'spark', 'progress')),
  spec TEXT NOT NULL DEFAULT '{}',        -- JSON (render.Spec)
  updated_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_screens_source ON screens(source_id);
CREATE INDEX idx_screens_app ON screens(app_id);

CREATE TABLE device_apps (
  device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  app_id TEXT NOT NULL,                   -- a user app's id or a catalog id ('clock'), so no FK
  pos INTEGER NOT NULL,
  PRIMARY KEY (device_id, app_id)
);
CREATE INDEX idx_device_apps_app ON device_apps(app_id);

CREATE TABLE app_installs (
  app_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  at TEXT NOT NULL,
  PRIMARY KEY (app_id, device_id)
);

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);  -- local-dev SECRET_KEY fallback only
```

Every index, unique constraint and foreign key from the old schema is kept, except those on
the legacy tables in 5.4.

### 5.3 Type changes

| Column(s) | Old | New |
|---|---|---|
| `*.created_at`, `updated_at`, `sessions.expires_at`, `claims.expires_at` | INTEGER, Unix seconds | TEXT, `2026-01-31T09:30:00Z` (UTC) |
| `devices.last_seen`, `sources.last_fetched_at`, `apps.published_at`, `app_installs.at` | INTEGER, where `0` meant never / unpublished / unknown | TEXT ISO 8601, `NULL` for the 0 case |
| `devices.flipped` | INTEGER 0/1 | INTEGER 0/1 + `CHECK` |
| `sources.kind`, `screens.layout` | free TEXT, validated in Go | TEXT + `CHECK` (a new layout later needs a table-rebuild migration) |
| IDs | TEXT (random hex) | TEXT, unchanged |
| `spec`, `last_data` | JSON in TEXT | unchanged |

No rows are converted (fresh start). The change is in how `store.go` reads and writes these
columns. Columns are read with
`COALESCE(unixepoch(col), 0)` and written as ISO strings, so the `int64` Unix fields in
`engine` and `web` (with 0 meaning never) don't change. These queries change:
`DueSources` (`last_fetched_at IS NULL OR unixepoch(last_fetched_at) + interval_sec <= unixepoch('now')`),
`UserBySession` and `UseClaim` (expiry), `UnclaimedDeviceByCode` (15-minute window),
`PublishedApps`, `InstallableApp` and `Unpublish` (`published_at IS NOT NULL`), and the
`ORDER BY created_at` / `published_at` sorts (ISO text sorts in time order).

### 5.4 Dropped from the schema

`flows`, `device_screens`, `devices.flow_id`, `devices.rotate_sec`, `screens.device_id`,
`flows.device_id` and `meta.apps_migrated` only existed to feed `migrateToApps`, the
one-time move from device screens to apps. With a fresh database there's nothing to move,
so the new schema leaves them out. `migrateToApps`, the column-add list and
`TestMigrateToApps` are deleted.

### 5.5 Data copy (phase 3)

Skipped: there's no data to copy. The first deploy creates an empty `app.db` from the
migrations, and nothing is uploaded in the Database tab.

### 5.6 Stored files

None. The bucket and Storage tab aren't needed.

---

## 6. What changes for users (for approval)

1. **The site renders in the browser.** URLs, layout and styles stay the same. Pages show
   a brief loading state while their `/api` call returns. A logged-out visit to a
   protected page now redirects in the browser after `/api/me` answers, not with a server
   303. The site no longer works with JavaScript off (only the htmx extras needed it
   before).
2. **Everyone starts fresh** (decided). Accounts, sources, apps and push URLs are created
   again on the new site.
3. **Boards are flashed from the new site.** The board that was on the laptop points at the
   laptop's LAN IP, so it needs re-flashing (Devices → Add a device). The flasher writes
   `PUBLIC_URL` into each board, so flash real boards only once `PUBLIC_URL` is the final
   `https://maker.app.rabeeta.xyz`, not a test address.
4. **During each deploy, two releases overlap for a few seconds**, so a source can be
   fetched twice. Boards reconnect to the new release within a second or two.

No feature is cut.

---

## 7. Decisions

All settled on 2026-10-24. The rule given with them: no change to how the product works
and no real downgrade of an existing feature. None of these cause one.

| # | Decision | Outcome |
|---|---|---|
| D1 | Timestamps: ISO 8601 TEXT as the brief's type rules say, or keep Unix INTEGER as the code uses today | **ISO TEXT.** There's no data to convert, and the change stays inside `store.go` SQL (5.3). Invisible to users. |
| D2 | Source poller: in-process worker goroutine (keeps the 15 s and 30 s intervals), or a Sideyard job every minute (drops intervals under 60 s) | **Worker goroutine.** A job would slow 15 s and 30 s sources to 60 s, which is a downgrade. AGENTS.md line 7 allows "goroutines for background work"; line 24 ("scheduled work runs through Sideyard, not goroutine timers") is about cron-style work. AGENTS.md gets a project note saying why the poller isn't a job. |
| D3 | Leave the legacy tables and columns out (5.4) | Decided by the fresh start. |
| D4 | `SECRET_KEY`: put a new random key (`openssl rand -hex 32`) in the Variables tab. On Sideyard (detected by `SIDEYARD_TOKEN` being set) the app won't start without it, so a release missing the key fails its health check and never goes live. Local dev keeps the generate-and-store-in-`meta` fallback. | **Yes.** Today (`main.go:85`) the key is stored in the same file as the tokens it encrypts, so anyone with a copy of `app.db` (Database tab downloads, backups) can read every saved API token. Keep a copy of the key in a password manager: losing it makes saved tokens unreadable. |
| D5 | Rate limits key on the rightmost `X-Forwarded-For` entry (added by Sideyard's proxy) instead of the leftmost (`auth.go:134` today), which clients can forge | **Yes.** Checked on the first deploy (section 9). |
| D6 | Rename espHog to makerApp / makerDevice | **Yes.** Go module `esphog` → `makerapp` (binary is `app`). Session cookie `esphog_session` → `makerapp_session` (fresh start, so nobody is logged out). Fetch `User-Agent` `espHog/1.0` → `makerApp/1.0`. WebSocket subprotocol: firmware 3.1.1 sends `makerdevice`, and the server accepts `makerdevice`, `esphog` (boards still on 3.1.0) and `arduino`. Comments, README, Makefile, landing page, `platformio.ini`, `portal.h` and the Lua `library.json` say makerApp / makerDevice. `reservedNames` keeps `esphog` so nobody can take the old name. |
| D7 | The repo's default branch is `master`, not `main`. The workflow ends up on `master`. | **Yes.** It keeps `[sideyard]` while testing, as the brief says. |
| D8 | Change the firmware's built-in server (`firmware/src/config.h` `DEFAULT_SERVER`, `config.cpp` fallback) to `maker.app.rabeeta.xyz`. Rebuild with `make firmware` (PlatformIO is installed) and commit the new `.bin`s, bumping the version to 3.1.1 so the site can tell the builds apart. | **Decided (2026-10-23):** `maker.app.rabeeta.xyz` is the permanent domain, and boards will be re-flashed from the new site. The README and code comments get the new domain too. |

---

## 8. Order of work and risks

### Order

**Phase 2** (branch `sideyard`; `master` and the old app stay as they are)
1. `git mv server/{internal,e2e_test.go,go.sum} .` and merge `go.mod`. Add the contract
   files. Merge the `Makefile` (starter targets plus `firmware`, `games-check`, `test`) and
   `.gitignore`. Wire `engine` into the starter `main.go` (poller on the signal context,
   close board sockets on shutdown). Remove autocert, libsql, `ADDR` and `DATABASE_URL`.
   Rename the module and the other names in D6; the firmware side ships with D8.
2. Write `db.go` migrations (5.2) and convert `store` (5.3). Delete the legacy migrator.
   The old template UI still runs on the new store at this point, and `go test` is green.
3. Add the JSON API route by route, ticking section 4. Extend `e2e_test.go` to drive the
   API and the device WebSocket.
4. Build `web/`: layout and nav, `DeviceScreen` + app art, `JsonTree`, then pages in order:
   auth, devices, device, settings, flash, pair, sources, source editor, apps, app, screen
   editor. Copy `style.css` and `static/ewt` across.
5. Mount the frontend, keep the `/screens` 303, and delete templates, htmx, `app.js`,
   `data.js` and the HTML rendering code, listing each deletion here. Rewrite the README
   deploy sections.

### Progress

- [x] **Step 1** (2026-10-24). `server/` moved to the root, module renamed `makerapp`.
  Added from the starter: `db.go`, `jobs.go` (no jobs), `email.go`, `AGENTS.md` (plus
  project notes), `CLAUDE.md`, `scripts/deploy.sh`, `.github/workflows/deploy.yml` (on
  `sideyard` for now). `Makefile` and `.gitignore` merged (`/app` is anchored to the root).
  `main.go` follows the starter: `127.0.0.1:$PORT`, `/healthz` pings the DB, the poller
  runs on the signal context, boards get "going away" (at most 1 s) before `Shutdown`, and
  on Sideyard a missing `SECRET_KEY` stops startup (D4). Removed: autocert, libsql/Turso,
  `ADDR`, `DATABASE_URL`, `AUTOCERT_DOMAIN`, `CERT_DIR`, and the web package's own
  `/healthz`. D5 (`clientIP` takes the last `X-Forwarded-For` entry) and the server side of
  D6 (cookie, `User-Agent`, `makerdevice` subprotocol alongside `esphog`) are in.
- [x] **Step 2** (2026-10-24). The schema in 5.2 is the `migrations` list (17 entries).
  `store.Open` became `store.New(db)`. Times are written with `iso()` and read with
  `unixepoch()`; the migrator, `migrateToApps` and `TestMigrateToApps` are gone.
  `DeleteSource` relies on `ON DELETE SET NULL` instead of clearing `screens.source_id`
  itself (that was only for Turso). `app_installs.at` is `NOT NULL`, since nothing predates
  the counter on a fresh database. `TestPublishing` moved to `db_test.go` next to the
  schema, with new tests for session and claim expiry, the pairing window and due sources.
  Checked: `go vet`, `go test ./...`, `make games-check`, and a local run (health check,
  pages, `/_jobs` 401, exit 0.02 s after SIGTERM, refusal without `SECRET_KEY` on Sideyard).
- [ ] Step 3: JSON API
- [ ] Step 4: `web/` React app
- [ ] Step 5: mount the frontend, delete the templates, rewrite the README
- [ ] D8 + firmware side of D6: `DEFAULT_SERVER`, `makerdevice` subprotocol, comments,
  version 3.1.1, `make firmware`

**Phase 3:** skipped (fresh start, 5.5).

**Phase 4:** the AGENTS.md checks, then `make web` and
`DATABASE_PATH=<copy> go run .`. Check `/healthz`, then each flow: sign up / log in / log
out, flash page (manifest and `config.bin` carry the claim and `PUBLIC_URL`), a board
connecting (simulated `hello` in the tests, plus the real board on USB, section 9),
pairing by code, sources (http / static / push, fetch now, explorer), apps (new, rename,
cycle, reorder, publish, unpublish, install, uninstall), screen editor (live preview,
inserting paths), device settings and actions, and the OTA URL. Then the launch steps for
a new deployment: create the project, set the variables, set the GitHub secret and
variables, deploy from `sideyard`, add the domain `maker.app.rabeeta.xyz` to the project
(its `A` record already points at `3.85.79.112`; keep it DNS only, not proxied), test,
then merge to `master` and point the workflow at `master`.

### Risks

| Risk | Mitigation |
|---|---|
| UI rewrite regressions. The screen editor (cursor insertion, live preview, explorer) and source editor (presets, header rows, secret params) are the most intricate. | Port page by page against the checklist, reuse `style.css` unchanged, walk every flow in phase 4. |
| Breaking flashed boards by changing a device route, the WebSocket protocol or the domain | Device handlers move without edits. `TestDeviceFlow` stays and guards them. The domain doesn't change. The subprotocol rename (D6) is additive: the server still accepts `esphog`. |
| `SECRET_KEY` changed or lost after launch: saved tokens silently stop decrypting (`OpenSecret` ignores errors) and sources start failing with 401s | Production refuses to start without `SECRET_KEY` (D4). The launch steps say to keep a copy of it outside Sideyard. |
| `0` vs `NULL` timestamp bugs, such as never-fetched sources never being due | The conversion is only in `store.go`. Tests cover due sources, session and claim expiry, and the pairing window. |
| Two pollers during the deploy overlap | Writes are idempotent. Worst case is one extra fetch per due source. |
| The `.gitignore` pattern `app` (from the contract) ignores any file or directory named `app`, anywhere | Nothing in `web/src` or `internal/` gets named `app`. |
| Firmware: CI embeds whatever `.bin`s are committed | Same as today. `make firmware` then commit. |

---

## 9. Open items

Settled: fresh start, no data to copy, new deployment at `maker.app.rabeeta.xyz`, all of
section 7, and the name (D6).

Answered on 2026-10-24:

- **Dashboard:** `SIDEYARD_URL=https://sideyard.app.rabeeta.xyz`. It serves the Sideyard
  dashboard from `3.85.79.112`, the same server `maker.app.rabeeta.xyz` points at.
- **WebSockets on `/ws`:** supported. The Sideyard AGENTS.md (line 7) says the Go server
  handles "APIs and WebSockets".
- **The board:** plugged in on USB as `/dev/cu.usbmodem1101`. It's device `B8F862D78BA4`,
  a `feather-esp32s3-rtft` on firmware 3.1.0, still pointed at the laptop
  (`192.168.68.64`). Phase 4 tests it against the local server. It's re-flashed from the
  live site only once `maker.app.rabeeta.xyz` is up.

Checked on the first deploy, not blocking Phase 2:

1. **CPU architecture.** The build defaults to amd64, like this Makefile and the other
   Sideyard apps. If the server is ARM, that release fails its health check and never
   goes live; then set `SIDEYARD_ARCH=arm64` and deploy again.
2. **`X-Forwarded-For`** (D5). Log it once on the deployed app and confirm the rightmost
   entry is the visitor's IP.

Needed from you at launch (Phase 4): create the `maker` project in the dashboard, a deploy
token for `SIDEYARD_TOKEN` (GitHub secret), and the `SECRET_KEY` value (D4).
