# espHog

Put any API on the little screen on your desk. espHog is one Go binary that runs
the website, the device connection, the data poller and the firmware flasher for
a DeskHog-style board (Adafruit Feather ESP32-S3 Reverse TFT: 240×135 screen,
buttons D0/D1/D2).

```
maker.rabeeta.xyz  ── one binary (bin/esphog) ──────────────────────────────
  Website        sign up · flash a board from the browser · data · apps
  /ws            devices keep a WebSocket open; changes are pushed instantly
  Poller         runs each data source on its interval, re-renders, pushes
  Firmware       baked in; served to the browser flasher and for OTA updates
  SQLite/Turso   one file locally, or libsql:// for Turso
        ▲
        │ wss://maker.rabeeta.xyz/ws (the device dials out, works behind any router)
  ESP32-S3 board: WiFi → hello → Apps drawer → opens apps, pages screens
```

## Use it

1. **Sign up** at the site.
2. **Devices → Add a device**: plug the board in over USB (Chrome or Edge), click
   **Install**, and enter your WiFi details when asked. The flasher writes a small
   settings block containing a one-time claim token, so the board shows up in your
   account by itself, with no ID or code to type.
3. **Data → New source**: any HTTP request with a token, such as PostHog, HubSpot or
   your own API. Presets only prefill the form. espHog fetches it on the interval
   you choose and keeps the latest JSON. There are also *Fixed data* (type JSON in)
   and *Push* (POST JSON to a private URL from scripts or Zapier) sources.
4. **Apps → New app**, then **New screen** inside it: pick a layout (big number,
   list, text, sparkline, progress) and click fields from the data panel to
   insert `{{ path }}`. Saving updates every board with the app within a second.
5. **Install** the app on a board, from the app's page or the device's Apps tab.
   The built-in **Clock** installs the same way. A new board starts with no apps.

### Templates

Text fields take [gjson paths](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)
in `{{ }}` with optional formatters:

| Example | Output |
|---|---|
| `{{ results.0.0 \| compact }}` | `1.2M` |
| `{{ bitcoin.usd \| money }}` | `$83.5k` |
| `{{ change \| fixed:2 \| signed }}%` | `+0.17%` |
| `{{ rows.#.amount \| sum \| num }}` | `12,350` |
| `{{ results \| len }}` | `10` |
| `{{ created_at \| ago }}` / `\| date` / `\| time` | `5m ago` / `Oct 1` / `14:05` |
| `{{ @ago }}`, `{{ @time }}` | when the source was fetched, current time |

Also: `num`, `pct` (adds %), `pct100` (×100 then %), `upper`, `lower`, `avg`,
`min`, `max`, `first`, `last`. For list screens, row label and value are
evaluated against each item, e.g. `{{ properties.dealname }}`.

### On the board

The board shows an **Apps** drawer: your installed apps in order, then the
built-in **Settings**. The buttons always do the same thing:

| | Drawer | Inside an app |
|---|---|---|
| **Top** (D0) | up | previous screen |
| **Middle** (D1) | open | back to the drawer |
| **Bottom** (D2) | down | next screen |

With the screen set upside down (device Settings → Screen direction), D2 is the
top button and takes over D0's job, so the top button always means up.

An open app stays on screen, also across restarts, until you press D1. Long lists
and texts are split into pages of five rows, and the dots in the corner show
where you are. An app can also turn its pages by itself (**Cycle screens**).
Data refreshes on each source's interval; there are no per-button actions.

### Publishing

On an app's page, **Publish** puts it in **All apps** for anyone to install,
straight away (no review). The first time, you pick a public name; apps show as
**@name**. People who install it see the app's data, fetched with your sources,
and your edits reach their boards immediately. **Unpublish** takes it off the
list and off other people's boards. Every board that has ever installed an app
counts as an install, and All apps is sorted by that.

### makerApp's apps

These are in All apps too, published as **@makerInc**.

| App | Buttons |
|---|---|
| **Clock** | time and date from the board's clock, works offline |
| **Pomodoro** | middle starts / pauses, bottom skips to the next phase, top restarts it; keeps counting while you're in other apps (firmware 3.1) |
| **Flappy Hog** | any button flaps |
| **Paddle** | top / bottom move your paddle, middle starts or pauses |
| **Hog Runner** | middle jumps, bottom ducks |
| **Snake** | top turns left, bottom turns right, middle pauses |
| **Breakout** | top / bottom move the paddle, middle launches |
| **Whack-a-Hog** | press the button for the row the hog is in |
| **Simon** | repeat the pattern on the three pads |
| **Stacker** | any button drops the sliding row |
| **Hog Invaders** | top / bottom move your ship, middle fires |

Everything except Clock is a Lua script in `server/internal/games` (how to
write one is in its README). On firmware 3.0 and later, installing one downloads
it to the board and uninstalling deletes it, and a changed script reaches boards
as a new version without a firmware update. A game runs in its own Lua state that only
exists while it's open, with a memory cap and a time limit per frame. In every
game, holding the middle button for a second goes back to the drawer, and best
scores are kept on the board. Boards on 2.1–2.3 have the games built in instead.
Flappy Hog and Paddle are ported from PostHog's
[DeskHog](https://github.com/PostHog/DeskHog); see `THIRD_PARTY_NOTICES.md`.

**Settings** has two pages; top / bottom switch between them. **WiFi** shows the
network, signal, IP and firmware. **Storage** shows how full the firmware space,
the downloaded apps (128 KB at the end of flash, outside the partition table) and
the internal memory are.

The board takes at most 15 KB of app content per update (the WebSockets library
fixes that limit, so the build flag in platformio.ini has no effect). The
device's Apps tab on the website shows the current size and warns from 80%. Holding **D1** on the WiFi page
for 6 seconds makes the board forget its WiFi and start the setup hotspot
(`makerDevice-XXXX`). The same hold works on the status screens shown before the
board reaches the server.

## Run locally

```sh
make            # builds firmware (PlatformIO) + server → bin/esphog
./bin/esphog    # http://localhost:8080
```

On localhost the flasher points boards at your computer's LAN IP
(`ws://192.168.x.x:8080/ws`), so a board on the same WiFi connects to your laptop.
Browser flashing works on `localhost` and on HTTPS sites only.

## Deploy on Sideyard

espHog runs on [Sideyard](../sideyard.io) as a project named `maker`, at `maker.rabeeta.xyz`.

1. In the Sideyard dashboard, create the project `maker` and set these variables:
   `PUBLIC_URL=https://maker.rabeeta.xyz`, plus `SECRET_KEY` if you use one, and
   `TZ` (for example `Asia/Kolkata`) if you want server time in your zone.
   Leave `ADDR` and `AUTOCERT_DOMAIN` unset: Sideyard sets `PORT` and handles HTTPS.
2. Put the dashboard's values in `.env.deploy`:
   ```sh
   SIDEYARD_URL=https://sideyard.rabeeta.xyz
   SIDEYARD_PROJECT=maker
   SIDEYARD_TOKEN=sy_...
   ```
3. `make deploy` (add `SIDEYARD_ARCH=arm64` for an ARM server) builds the firmware
   and a Linux binary, then uploads it.

The database lives in the project's data folder as `data/esphog.db` and survives
deploys. Boards keep their WebSocket through other projects' deploys; a deploy of
espHog itself makes them reconnect to the new release.

## Deploy to maker.rabeeta.xyz (without Sideyard)

```sh
make linux      # bin/esphog-linux-amd64 and bin/esphog-linux-arm64
scp bin/esphog-linux-amd64 server:/opt/esphog/esphog
```

**Option A: the binary handles HTTPS itself** (ports 80 and 443 open, DNS pointing at the box):

```sh
AUTOCERT_DOMAIN=maker.rabeeta.xyz SECRET_KEY=$(openssl rand -hex 32) ./esphog
```

**Option B: behind Caddy or nginx.** Run it on :8080 with
`PUBLIC_URL=https://maker.rabeeta.xyz` and proxy to it. The proxy must pass
WebSocket upgrades on `/ws` and set `X-Forwarded-Proto`. Caddy does both by
default: `maker.rabeeta.xyz { reverse_proxy localhost:8080 }`.

Example systemd unit (`/etc/systemd/system/esphog.service`):

```ini
[Unit]
Description=espHog
After=network-online.target

[Service]
WorkingDirectory=/opt/esphog
ExecStart=/opt/esphog/esphog
Environment=PUBLIC_URL=https://maker.rabeeta.xyz
Environment=SECRET_KEY=change-me-to-a-long-random-string
Restart=always

[Install]
WantedBy=multi-user.target
```

### Settings

| Variable | Default | |
|---|---|---|
| `ADDR` | `:8080` | listen address (ignored with `AUTOCERT_DOMAIN`) |
| `DATABASE_URL` | `data/esphog.db` | or `libsql://<db>-<org>.turso.io?authToken=<token>` for Turso |
| `PUBLIC_URL` | from the request | the URL boards and browsers use |
| `SECRET_KEY` | generated, stored in DB | encrypts saved API tokens; set it in production |
| `ALLOW_SIGNUP` | `true` | `false` closes registration |
| `ALLOW_PRIVATE_FETCH` | `false` | let sources and webhooks call LAN/localhost addresses |
| `AUTOCERT_DOMAIN` | | built-in Let's Encrypt on :443 |
| `LOG_LEVEL` | `info` | `debug` logs requests |

## First flash on a board running CircuitPython

Boards that ship with CircuitPython or UF2 need to be put into the ROM bootloader
once: **hold D0 (BOOT), tap RESET, release D0**, then click Install. After that,
espHog's firmware uses the chip's built-in USB serial, so later re-flashes from the
site reset the board automatically. Updates can also go over WiFi from the device
page (**Update firmware**).

## Layout

```
firmware/                 PlatformIO project (Arduino, Adafruit GFX/ST7789, ArduinoJson, WebSockets)
  src/main.cpp            state machine: WiFi → server → pairing → Apps drawer, apps, Settings, OTA
  src/display.cpp         the five layouts, clock, drawer, Settings, status/pairing screens, toasts
  src/games.cpp           runs the open game; game_vm (Lua state + limits), lua_api (the game API)
  src/game_store.cpp      downloaded games: LittleFS in the last 128 KB of flash, sync with the server
  lib/lua                 Lua 5.4.7, trimmed
  tools/gamecheck.cpp     runs every game on a computer (make games-check)
  src/improv.cpp          Improv-WiFi-over-serial (WiFi setup from the browser)
  src/portal.cpp          fallback "makerDevice-XXXX" hotspot with a WiFi form
  src/config.cpp          NVS + the "hogcfg" partition the flasher writes
  partitions.csv          OTA layout + hogcfg at 0x3D0000
server/
  main.go                 config, wiring, HTTP/HTTPS
  internal/engine         device WebSocket hub, poller, push, installed apps → device state
  internal/games          the downloadable Lua games (embedded; served at /games/<id>/<hash>.lua)
  internal/render         templates → device payload, data explorer paths
  internal/store          SQLite/Turso schema and queries (apps, screens, installs)
  internal/fw             embedded firmware, ESP Web Tools manifest, config image
  internal/web            pages (Go templates + htmx), auth, flashing, pairing
```

### Device protocol (JSON over WebSocket)

Device → server: `hello {id, token, code, fw, claim}`.
Server → device: `state {name, brightness, flip, ox, oy, tz, apps[{id, name, cycle, screens[]}]}`,
`pair {code}`, `toast {text}`, `ota {url}`, `identify`, `reboot`, `unpaired`,
`error {text}`. Built-in apps arrive as a screen whose layout is the app's ID
(`clock`). Boards on firmware before 2.0 get a flat `nodes` playlist instead.
A device proves itself with a secret it generates on first boot. Once claimed,
a device with the same ID but a different secret is refused unless it carries a
claim from the same owner, which happens when the owner re-flashes it from the site.

## License

MIT, see `LICENSE`. Code from other projects is listed in `THIRD_PARTY_NOTICES.md`.
