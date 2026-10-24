# Building makerApp apps with Claude or Codex (MCP)

Status: **Phase 1 done (2026-10-24).** Phases 2 and 3 not started. Decisions in section 8.

Phase 1 as built:
- `internal/library`: the shared rules (sources, apps, screens, installs, per-account caps);
  the website's handlers call it too.
- `internal/web/oauth.go`: discovery (RFC 9728 and 8414 metadata), client ID metadata
  documents and dynamic registration, the consent API, the token endpoint (PKCE S256,
  single-use codes, rotating refresh tokens; a replayed one revokes the grant), connected
  apps and Disconnect. Tables appended to `migrations`.
- `internal/mcpserver`: Streamable HTTP, stateless, JSON responses. 17 tools: `get_overview`
  (instead of separate list tools), `get_guide`, `list_templates`, `try_request`,
  `get_source`, `create_source`, `update_source`, `fetch_source`, `get_app`, `create_app`,
  `update_app`, `preview_screen`, `add_screen`, `update_screen`, `reorder_screens`,
  `install_app`, `uninstall_app`. The SDK's DNS-rebinding check is off, because Sideyard's
  proxy reaches the app on 127.0.0.1 with the public Host (every request carries a token).
- `internal/render`: the checks, measured with the board's own font metrics (generated from
  the firmware's Adafruit GFX fonts by `go generate ./internal/render`), and the layout guide
  in the instructions.
- Website: **Build with AI** (`/connect`), the consent page (`/oauth/authorize`), "Made with"
  labels.
- Tests: `mcp_test.go` (discovery, registration, consent, PKCE, code reuse, the tools, the
  checks, install reaching a simulated board, other people's apps refused, refresh rotation
  and replay, Disconnect) and `internal/render/check_test.go`.
- Real clients, locally: a headless Claude Code run built a Bitcoin app from one sentence
  (it explored CoinGecko, used previews, got no checks, installed it), and Codex read the
  account through the same endpoint.

makerApp gets an MCP connector. A user connects Claude, Codex or another MCP client to
their makerApp account, says what they want on their board, and the AI builds it, shows
it, and installs it.

Checked against the MCP spec 2026-07-28 (authorization, elicitation), the MCP Apps
extension (UI in the chat), the official Go SDK (`github.com/modelcontextprotocol/go-sdk`)
and the Codex CLI source.

---

## 1. What a really good experience looks like

1. **Connecting takes a minute.** makerApp's **Build with AI** page has one URL,
   `https://maker.app.rabeeta.xyz/mcp`, with copy-paste steps for Claude (web, desktop,
   mobile), Claude Code, Codex and ChatGPT. The AI opens a makerApp page; you log in and
   press **Allow**. The page then shows "Connected: Claude · used 2 min ago".
2. **You say it in plain words.** "Show today's PostHog signups and the Bitcoin price."
   The AI already knows makerApp: your boards, your data sources, the built-in templates,
   what fits on a 240×135 screen. It only asks what it can't find out ("Which PostHog
   project?").
3. **You see it before it's on the board.** Every screen the AI makes shows up in the chat
   as a picture of the board. In Claude and ChatGPT it's a live widget; in Codex and Claude
   Code it's an image. The AI looks at the picture too, and fixes clipped text or empty
   values before showing you.
4. **API keys never touch the chat.** When an API needs a key, the chat shows a makerApp
   link. You paste the key on makerApp's page, and the AI carries on.
5. **Install from the chat.** The widget has **Install on makerDevice 8BA4**, or you just
   say "install it". The board changes within seconds.
6. **Keep refining, live.** "Make the number bigger", "add the 24 h change", "turn pages
   every 10 s": each change reaches the board within a second.
7. **Everything is visible and undoable.** makerApp keeps a list of what the AI did, with
   **Undo** on each change. Apps carry a "Made with Claude" label. **Disconnect** cuts the
   AI off at once.
8. **It fixes things too.** "My weather screen is blank": the AI reads the source's
   status and error, then fixes the path, the URL or the interval.

## 2. What the AI can and can't do

**Can:** see the user's boards (name, online or not, firmware, installed apps), apps and
data sources; try a request without saving it; create and edit data sources, apps and
screens; fetch a source and read its data; preview screens; install and uninstall apps on
the user's boards, limited to the user's own apps and makerApp's built-in ones.

**Can't** (these stay in makerApp): install apps other people published, publish, delete
anything (Undo covers mistakes), restart or update a board, see or set saved API keys.

Why: the AI reads data from other people's APIs, and that data could contain text aimed
at the AI. Limiting it to adding, editing and installing your own apps, all visible in
makerApp and undoable, limits what a bad instruction could do. Claude and Codex also ask
you to approve changes before they run, unless you've told them not to.

## 3. What the server offers

### Instructions and the guide

What makes an AI good at makerApp is knowing the board. The server's **instructions**
(sent when the AI connects) and a `guide` resource cover:
- the five layouts, with how many characters each field fits (measured from the board's
  font);
- 5 rows a page, the three buttons, cycling;
- `{{ path | format }}` templates, every formatter, and `device.*` fields like the clock;
- the 15 KB limit, sensible refresh intervals, and simple design rules (one big number,
  short titles, accent colors that read on black);
- worked examples.

### Tools

| Group | Tools | Notes |
|---|---|---|
| Overview | `get_overview` | One call for the whole picture: boards, apps, sources, and what's installed where. New users get "no board yet: flash one at …/flash" |
| Boards | `list_devices` | Name, online, firmware, installed apps, apps data size |
| Data | `list_templates`, `try_request`, `list_sources`, `get_source`, `create_source`, `update_source`, `fetch_source` | `try_request` fetches a URL without saving, to explore an API first. Answers carry status, a trimmed JSON sample and field paths, never keys. A 401/403 starts the key flow (section 5) |
| Apps | `list_apps`, `get_app`, `create_app`, `update_app` | Name, description, cycle time; answers with the makerApp link |
| Screens | `preview_screen`, `add_screen`, `update_screen`, `reorder_screens` | Every answer has the rendered screen (picture and values) and the checks in the next table |
| Board | `install_app`, `uninstall_app` | Only the user's own apps and built-in ones, only on the user's boards. Answers with the board's data size and a warning near 15 KB |

Read-only tools carry `readOnlyHint`, and writes say whether they're idempotent, so
clients can skip approval prompts for reads.

### Checks that teach

Each screen answer says what's wrong, in words the AI can act on:

| Check | Example message |
|---|---|
| Text too long for the layout | "Title is 31 characters; this layout shows about 20. Shorten it." |
| Path didn't resolve | "`results.0.count` isn't in the data. Close matches: `results.0.0`, `results.0.total`" |
| Empty value | "Main value renders empty: the source returned an error (401)." |
| List problems | "List path points at an object, not a list." / "Showing 5 of 40 rows a page; set Max rows." |
| Board capacity | "This app adds 4.1 KB; makerDevice 8BA4 would be at 13.9 of 15 KB." |
| Refresh too often | "Every 15 s against a free API may get rate-limited; 5 min is usual." |

### Pictures of the screen

A Go renderer draws a screen as the board would: 240×135 PNG, same layouts, drawn with the
board's own font data (the Adafruit GFX FreeSans tables the firmware uses), so the picture
and the length checks match the board pixel for pixel. Screen answers include the PNG as
an MCP image for the model, and as the fallback in clients without widgets.

### The widget (MCP Apps)

`get_app`, `add_screen`, `update_screen` and `preview_screen` name a `ui://` resource: one
self-contained HTML file built from the website's own `DeviceScreen` component. In Claude
and ChatGPT it shows the app's screens as on the board, with **Install on <board>** (calls
`install_app` through the host) and **Open in makerApp**. Clients without widgets get the
text and image.

### Prompts

Ready-made starting points that show up as commands in the client:
- `build_app`: an idea → a finished, installed app;
- `dashboard_from_api`: a URL or service → screens for it;
- `fix_screen`: a screen that looks wrong → found and fixed.

### Resources

`makerapp://guide`, `makerapp://overview` and `makerapp://apps/{id}`, for clients that let
users attach context.

## 4. Sign-in and transport

Everything stays inside the one Go binary and SQLite file, so it fits Sideyard as is.

**Endpoint.** `/mcp`, Streamable HTTP from the official Go SDK, stateless: each request
stands alone (the direction of the 2026 spec), so deploys don't break connected clients.

**OAuth 2.1**, as the MCP spec requires, with makerApp as its own authorization server:

| Piece | What it is |
|---|---|
| `/.well-known/oauth-protected-resource` (also under `/mcp`) | RFC 9728 metadata naming makerApp as the authorization server (the SDK serves it) |
| `/.well-known/oauth-authorization-server` | RFC 8414: endpoints, `code_challenge_methods_supported: ["S256"]`, `client_id_metadata_document_supported: true`, `registration_endpoint`, grants `authorization_code` and `refresh_token`, public clients |
| Client identification | Client ID Metadata Documents (the spec's preferred way; makerApp fetches the client's JSON through the SSRF-guarded fetcher, HTTPS only, 5 KB, 5 s, cached), with Dynamic Client Registration (`POST /oauth/register`) as the fallback |
| `GET /oauth/authorize` | React consent page (login first if needed). **Allow** returns the redirect with a one-time code: 10 minutes, single use, bound to the client, redirect URI, PKCE challenge and resource |
| `POST /oauth/token` | Code plus PKCE verifier → access token (1 hour) and refresh token (60 days, replaced on each use; replaying an old one revokes the grant). Opaque random tokens, stored hashed. `resource` must be the `/mcp` URL |
| `/mcp` guard | The SDK's `auth.RequireBearerToken` with makerApp's check; 401 with `WWW-Authenticate: Bearer resource_metadata="…"` starts sign-in in the client |

`/oauth/token`, `/oauth/register` and `/mcp` skip the cross-site check (it's for cookies).

**Tables** (appended to `migrations`): `oauth_clients`, `oauth_codes`, `oauth_grants` (user
× client: created, last used, revoked), `oauth_tokens` (hash, grant, kind, expiry),
`ai_activity` (who did what, with the state before it, for Undo), and `apps.made_by`.

**Shared rules.** Today the rules for saving a source, screen or app live in the website's
handlers. They move into one package (for example `internal/library`) that the website API
and the MCP tools both call, so both validate and push to boards the same way.

## 5. API keys

1. `create_source` or `fetch_source` gets 401/403, or the user says the API needs a key.
2. The tool answers with a URL-mode elicitation (2026 spec: `InputRequiredResult` with
   `mode: "url"`, which works statelessly) pointing at a one-purpose page:
   `/sources/<id>/key`: "Paste your PostHog key for 'Signups'". Clients without
   elicitation get the same link in the text.
3. The user saves the key there; it's encrypted like today. The client retries, the tool
   sees the key is saved, fetches the source and answers with the data.

The AI never sees the key, and the spec forbids asking for it any other way.

## 6. In makerApp

- **Build with AI** (`/connect`), linked from Apps: the URL with Copy, steps per client, and
  connected AIs with last use and **Disconnect**.
- **Activity**: what each AI did ("Claude added 'BTC' to Prices · installed Prices on
  makerDevice 8BA4"), newest first, with **Undo** (a created thing goes, an edit goes back,
  an install is removed).
- **Made with Claude** on app cards and the app page.
- The consent page and the key page.

## 7. Order of work

Each phase ships on its own, tested and deployed.

**Phase 1: connect and build.** The core loop works in every client.
1. Shared rules package (refactor; website behavior unchanged).
2. OAuth: metadata, both registration ways, consent, tokens, Disconnect.
3. `/mcp` with instructions, the guide, overview, data, app, screen and board tools, text
   previews and the checks (character limits from the board font's widths).
4. Build with AI page, Made with labels, key link to the source page.
5. Tests: OAuth flows (PKCE, code reuse, redirect mismatch, expiry, wrong resource, refresh
   reuse, Disconnect) and an end-to-end MCP test with the Go SDK's client: build an app,
   install it, see it reach a simulated board; installing someone else's app is refused.

**Phase 2: see it.**
1. The PNG renderer with the board's fonts; previews return the picture.
2. The MCP Apps widget with Install, in Claude and ChatGPT.

**Phase 3: trust and polish.**
1. Activity with Undo.
2. URL-mode key entry and the key page.
3. Prompts and resources.

**Each phase ends with a real-world check:** a fixed set of requests ("weather in
Kozhikode", "BTC and ETH prices", "GitHub stars for a repo", "PostHog signups" (needs a
key), "days until Dec 31", "my top HubSpot deals") run in Claude, Claude Code and Codex.
Measure: an app from one sentence, no edits in the website, no clipped text or empty values,
installed and working on the board. Tool descriptions and instructions get tuned until it
passes.

**Later**
- AI-written Lua apps (games, timers, tools): per-user scripts, a server-side check
  (syntax plus the same limits as `make games-check`, for example with a pure-Go Lua 5.4),
  and serving them to that user's boards.
- "Show it on the board now": a firmware command (3.2) to open a given app, so the AI can
  put a fresh screen in front of you.

## 8. Decisions

| # | Decision | Recommendation |
|---|---|---|
| M1 | The AI can install and uninstall on your boards, limited to your own apps and the built-in ones | **Decided (2026-10-24)** |
| M2 | API keys never go through the chat; you paste them in makerApp | Yes (the spec requires it) |
| M3 | Apps are data screens for now; AI-written Lua apps later | Yes |
| M4 | Open to every makerApp user, with rate limits and caps | Yes |
| M5 | No delete tool; Undo in makerApp covers mistakes | Yes |

## 9. Limits and safety

- Rate limits: tool calls per user (about 120 a minute), fetches and `try_request` (about
  30 a minute), OAuth endpoints per IP.
- Caps per user: apps, screens and sources (for example 100, 500 and 100).
- Outbound fetches keep the guard against private and LAN addresses.
- Redirect URIs match exactly; codes work once; Disconnect revokes at once.
- Logs record the tool, user and client, not the data.

## 10. Risks

| Risk | Plan |
|---|---|
| Clients differ in what they support (widgets, elicitation, prompts) | Every feature has a plain fallback: text, image, link. The core loop needs only tools and OAuth |
| The Go SDK may lag the 2026 spec (`InputRequiredResult`, MCP Apps metadata) | Check the SDK version first; write the JSON shapes by hand if needed (the SDK lets tools set `_meta` and raw results) |
| The PNG renderer drifts from the firmware's drawing | It uses the firmware's font data and layout numbers; a test renders each layout and compares it with stored pictures |
| AI quality depends on wording | The real-world check in each phase, and the checks that teach |
