// makerDevice firmware: connects to WiFi, keeps a WebSocket open to the
// makerApp server, and runs a small app launcher. The server sends the
// installed apps (each an ordered list of screens); the board lists them in
// an Apps drawer and pages through them on its own. D1 opens and closes an
// app, D0/D2 move.
// Settings is built in and always last in the drawer.

#include <Arduino.h>
#include <ArduinoJson.h>
#include <HTTPUpdate.h>
#include <WebSocketsClient.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>

#include <vector>

#include "certs.h"
#include "config.h"
#include "display.h"
#include "game_store.h"
#include "games.h"
#include "improv.h"
#include "portal.h"
#include "sensors.h"

namespace {

enum class Mode { Setup, WifiConnecting, ServerConnecting, Pairing, Ready, Error };
// What Ready mode shows: the Apps drawer, an open app, or the built-in Settings.
enum class View { Drawer, App, Settings };

Mode mode = Mode::Setup;
WebSocketsClient ws;
bool wsStarted = false;
bool wsConnected = false;

JsonDocument state;       // last "state" message from the server: the installed apps
View view = View::Drawer;
int drawerSel = 0;        // highlighted drawer row: apps in order, then Settings
String openApp;           // ID of the app on screen
int page = 0;             // page within the open app (long lists and texts span several)
bool restored = false;    // reopened the last-used app after boot
bool shownClock = false;  // what's on screen, so the loop knows when to redraw
bool shownLive = false;
bool shownGame = false;
String activeGame;        // id + hash of the game on screen, "" when none
bool gamesSyncDue = false;  // installed games changed: fetch or delete game files
uint32_t gamesRetryAt = 0;  // when to retry a download that failed
int lastMinute = -1;            // clock redraws once a minute
uint32_t lastLiveDraw = 0;      // screens with device fields redraw periodically
uint32_t lastSettingsDraw = 0;  // Settings shows live signal strength and memory
int settingsPage = 0;           // 0 WiFi, 1 Storage
uint32_t lastCycle = 0;         // last automatic page turn
int holdLeft = 0;               // seconds of D1 hold left before WiFi reset, shown in Settings
bool dirty = true;

String toastText;
uint32_t toastUntil = 0;
String errorText;
String deviceName;
String pendingOta;
bool pendingReboot = false;
uint32_t wifiStartedAt = 0;
uint32_t serverSince = 0;  // when we started waiting for the server
volatile uint8_t wifiFailReason = 0;  // last disconnect reason from the WiFi driver

// Turns the driver's disconnect reason into something a person can act on.
String wifiReasonText() {
  switch (wifiFailReason) {
    case 0:
      return "";
    case WIFI_REASON_NO_AP_FOUND:
      return "Not found (needs 2.4 GHz)";
    case WIFI_REASON_AUTH_FAIL:
    case WIFI_REASON_4WAY_HANDSHAKE_TIMEOUT:
    case WIFI_REASON_HANDSHAKE_TIMEOUT:
    case WIFI_REASON_AUTH_EXPIRE:
      return "Wrong password?";
    case WIFI_REASON_ASSOC_FAIL:
    case WIFI_REASON_CONNECTION_FAIL:
      return "Network refused the board";
    default:
      return "Network error " + String(wifiFailReason);
  }
}
uint32_t lastInteraction = 0;  // last button press


// D0 is the BOOT button (pulled up, active low); D1/D2 are pulled down, active high.
struct Button {
  uint8_t pin;
  bool activeLow;
  bool down;
  uint32_t changedAt;
  bool longFired;
};
Button buttons[3] = {{0, true}, {1, false}, {2, false}};
constexpr uint32_t WIFI_RESET_HOLD_MS = 6000;
constexpr uint32_t SHORT_PRESS_MS = 1000;  // D1 acts on release when held for less than this

String hostOnly() {
  String base = configWebBase();
  int i = base.indexOf("://");
  return i < 0 ? base : base.substring(i + 3);
}

// ---- apps (sent by internal/engine BuildState) ----

JsonArrayConst apps() { return state["apps"].as<JsonArrayConst>(); }
int appCount() { return apps().size(); }

int appIndex(const String& id) {
  int i = 0;
  for (JsonObjectConst a : apps()) {
    if (id == (a["id"] | "")) return i;
    i++;
  }
  return -1;
}

JsonObjectConst currentApp() {
  int i = appIndex(openApp);
  return i < 0 ? JsonObjectConst() : apps()[i].as<JsonObjectConst>();
}

// A page is one screen, or one part of a long list or text.
struct Page {
  JsonObjectConst screen;
  int part;
};

std::vector<Page> appPages(JsonObjectConst app) {
  std::vector<Page> out;
  for (JsonObjectConst s : app["screens"].as<JsonArrayConst>()) {
    int n = screenPages(s);
    for (int p = 0; p < n; p++) out.push_back({s, p});
  }
  return out;
}

void openAppAt(int i) {
  openApp = apps()[i]["id"] | "";
  view = View::App;
  page = 0;
  lastCycle = millis();
  configSaveLastApp(openApp);
}

void openSettings() {
  view = View::Settings;
  settingsPage = 0;
  configSaveLastApp("");
}

// Back to the drawer, with the app just closed highlighted.
void closeApp() {
  drawerSel = view == View::Settings ? appCount() : max(0, appIndex(openApp));
  view = View::Drawer;
  holdLeft = 0;
  configSaveLastApp("");
}

void toast(const String& t, uint32_t ms = 2000) {
  toastText = t;
  toastUntil = millis() + ms;
  dirty = true;
}

void setMode(Mode m) {
  if (mode != m) {
    mode = m;
    dirty = true;
    if (m == Mode::ServerConnecting) serverSince = millis();
  }
}

void drawClockNow(JsonObjectConst screen) {
  time_t t = time(nullptr) + (long)(state["tz"] | 0);
  struct tm tmv;
  gmtime_r(&t, &tmv);
  char hm[6], date[24];
  strftime(hm, sizeof hm, "%H:%M", &tmv);
  strftime(date, sizeof date, "%a, %b %d", &tmv);
  lastMinute = tmv.tm_min;
  drawClock(screen["title"] | "", t > 1700000000 ? String(hm) : String("--:--"), date);
}

void renderDrawer() {
  std::vector<String> labels;
  for (JsonObjectConst a : apps()) labels.push_back(a["name"] | "App");
  labels.push_back("Settings");
  drawerSel = constrain(drawerSel, 0, (int)labels.size() - 1);
  drawMenu("Apps", labels, drawerSel, appCount() == 0 ? "Add apps at " + hostOnly() : "");
}

String oneDecimal(float v) { return String(v, 1); }

// Storage: firmware space, space for downloaded apps, and memory.
void renderStorage() {
  static uint32_t sketch = ESP.getSketchSize();  // reads the image once, so cache it
  uint32_t slot = ESP.getFreeSketchSpace();      // size of an app partition
  uint32_t heap = ESP.getHeapSize(), heapUsed = heap - ESP.getFreeHeap();
  std::vector<Meter> meters = {
      {"Firmware", oneDecimal(sketch / 1048576.0f) + " / " + oneDecimal(slot / 1048576.0f) + " MB",
       (float)sketch / slot},
      {"Apps", String(gameStoreUsed() / 1024) + " / " + String(gameStoreTotal() / 1024) + " KB",
       (float)gameStoreUsed() / gameStoreTotal()},
      {"Memory", String(heapUsed / 1024) + " / " + String(heap / 1024) + " KB", (float)heapUsed / heap},
  };
  drawMeters("Storage", meters, 1, 2);
  lastSettingsDraw = millis();
}

void renderSettings() {
  if (settingsPage == 1) return renderStorage();
  bool up = WiFi.status() == WL_CONNECTED;
  std::vector<String> rows = {
      "WiFi: " + cfg.ssid + (up ? "" : " (offline)"),
      up ? "Signal: " + String(WiFi.RSSI()) + " dBm" : String("Signal: none"),
      "IP: " + (up ? WiFi.localIP().toString() : String("-")),
      "Firmware " FW_VERSION "   ID " + cfg.deviceId.substring(8),
  };
  String footer = holdLeft > 0 ? "Keep holding... " + String(holdLeft) : String("Hold D1 6s to change WiFi");
  drawSettings("WiFi", rows, footer, holdLeft > 0, 0, 2);
  lastSettingsDraw = millis();
}

void renderApp(JsonObjectConst app) {
  std::vector<Page> pages = appPages(app);
  if (pages.empty()) {
    view = View::Drawer;
    renderDrawer();
    return;
  }
  page = constrain(page, 0, (int)pages.size() - 1);
  JsonObjectConst screen = pages[page].screen;
  String layout = screen["layout"] | "";
  if (isGameLayout(layout)) {
    shownGame = true;
    String id = screen["game"] | "", hash = screen["hash"] | "";
    if (activeGame != id + hash) {
      activeGame = id + hash;
      gameOpen(id, hash, app["name"] | "Game");
    }
    gameDraw();
    return;
  }
  if (layout == "clock") {
    shownClock = true;
    drawClockNow(screen);
    return;
  }
  JsonDocument filled;
  if (screen["live"] | false) {
    // Fill {$device fields} (clock, battery, ...) on a copy before drawing.
    filled.set(screen);
    for (const char* k : {"title", "value", "sub", "body"}) {
      if (filled[k].is<const char*>()) filled[k] = fillDeviceFields(filled[k].as<String>());
    }
    for (JsonArray row : filled["items"].as<JsonArray>()) {
      row[0] = fillDeviceFields(row[0].as<String>());
      row[1] = fillDeviceFields(row[1].as<String>());
    }
    screen = filled.as<JsonObjectConst>();
    shownLive = true;
    lastLiveDraw = millis();
  }
  drawScreen(screen, pages[page].part, page, pages.size());
}

void render() {
  shownClock = shownLive = shownGame = false;
  switch (mode) {
    case Mode::Setup:
      // Phone-friendly instructions: the hotspot works without any computer.
      drawStatus("WiFi setup (2.4 GHz only)", "1. On your phone, join WiFi:", "   " + portalSsid(),
                 "2. Open 192.168.4.1");
      break;
    case Mode::WifiConnecting:
      if (portalActive())
        drawStatus("Can't join " + cfg.ssid, wifiReasonText(), "Fix: join " + portalSsid() + ", then open 192.168.4.1");
      else
        drawStatus("Joining WiFi", cfg.ssid, wifiReasonText(), "Hold D1 6s to change WiFi");
      break;
    case Mode::ServerConnecting:
      if (millis() - serverSince > 20000)
        drawStatus("Can't reach server", "Server: " + hostOnly(), "WiFi: " + cfg.ssid,
                   "My IP: " + WiFi.localIP().toString());
      else
        drawStatus("Connecting", "WiFi: " + cfg.ssid, "Server: " + hostOnly(),
                   "My IP: " + WiFi.localIP().toString());
      break;
    case Mode::Pairing:
      drawPairing(cfg.code, hostOnly());
      break;
    case Mode::Error:
      drawStatus("Problem", errorText, "ID " + cfg.deviceId);
      break;
    case Mode::Ready:
      if (view == View::Settings) {
        renderSettings();
      } else if (view == View::App && !currentApp().isNull()) {
        renderApp(currentApp());
      } else {
        view = View::Drawer;
        renderDrawer();
      }
      break;
  }
  if (!shownGame && !activeGame.isEmpty()) {
    gameClose();  // frees the game's memory
    activeGame = "";
  }
  if (toastUntil && millis() < toastUntil) drawToast(toastText);
}

void sendJson(JsonDocument& doc) {
  if (!wsConnected) return;
  String out;
  serializeJson(doc, out);
  ws.sendTXT(out);
}

void sendHello() {
  JsonDocument d;
  d["type"] = "hello";
  d["id"] = cfg.deviceId;
  d["token"] = cfg.token;
  d["code"] = cfg.code;
  d["fw"] = FW_VERSION;
  d["board"] = "feather-esp32s3-rtft";
  d["ip"] = WiFi.localIP().toString();
  d["rssi"] = WiFi.RSSI();
  if (!cfg.claim.isEmpty()) d["claim"] = cfg.claim;
  sendJson(d);
}

void applyState(JsonDocument& doc) {
  // Keep the drawer highlight on the same app when the list changes.
  bool hadApps = !state["apps"].isNull();
  bool onSettingsRow = drawerSel >= appCount();
  String highlighted = onSettingsRow ? String() : String(apps()[drawerSel]["id"] | "");
  state = std::move(doc);
  if (!restored) {
    // After a restart, reopen the app that was on screen.
    restored = true;
    if (!cfg.lastApp.isEmpty() && appIndex(cfg.lastApp) >= 0) {
      openApp = cfg.lastApp;
      view = View::App;
      page = 0;
      lastCycle = millis();
    }
  }
  if (view == View::App && appIndex(openApp) < 0) closeApp();  // uninstalled while open
  if (view == View::Drawer && hadApps) {
    if (onSettingsRow)
      drawerSel = appCount();
    else if (appIndex(highlighted) >= 0)
      drawerSel = appIndex(highlighted);
  }
  displaySetBrightness(state["brightness"] | 100);
  sensorsSetTimezone(state["tz"] | 0);
  bool flip = state["flip"] | false;
  int ox = state["ox"] | 0, oy = state["oy"] | 0;
  if (flip != cfg.flipped || ox != cfg.offsetX || oy != cfg.offsetY) {
    configSaveDisplay(flip, ox, oy);
    displaySetOrientation(flip, ox, oy);
  }
  deviceName = state["name"] | deviceName;
  setMode(Mode::Ready);
  dirty = true;
}

void identify() {
  for (int i = 0; i < 3; i++) {
    displaySetBrightness(5);
    delay(150);
    displaySetBrightness(100);
    delay(150);
  }
  displaySetBrightness(state["brightness"] | 100);
  toast("Hello!");
}

void handleMessage(uint8_t* payload, size_t len) {
  JsonDocument doc;
  if (deserializeJson(doc, payload, len)) return;
  String type = doc["type"] | "";
  if (type == "state") {
    gamesSyncDue = true;
    applyState(doc);
  } else if (type == "pair") {
    setMode(Mode::Pairing);
  } else if (type == "welcome") {
    deviceName = doc["name"] | "";
    setMode(Mode::Ready);
  } else if (type == "toast") {
    toast(doc["text"] | "", doc["ms"] | 2000);
  } else if (type == "ota") {
    pendingOta = doc["url"] | "";
  } else if (type == "reboot") {
    pendingReboot = true;
  } else if (type == "identify") {
    identify();
  } else if (type == "unpaired") {
    configNewPairCode();
    state.clear();
    view = View::Drawer;
    drawerSel = 0;
    deviceName = "";
    sendHello();
  } else if (type == "error") {
    errorText = doc["text"] | "Server error";
    setMode(Mode::Error);
  }
}

void onWsEvent(WStype_t type, uint8_t* payload, size_t len) {
  switch (type) {
    case WStype_CONNECTED:
      Serial.println("makerDevice: server connected");
      wsConnected = true;
      displaySetOffline(false);
      dirty = true;
      sendHello();
      break;
    case WStype_DISCONNECTED:
      Serial.println("makerDevice: server disconnected / unreachable");
      if (wsConnected) {
        wsConnected = false;
        // A ready board keeps working offline (with a red dot): the drawer,
        // the last data and Settings. Apps refresh on reconnect.
        displaySetOffline(true);
        if (mode != Mode::Ready && mode != Mode::Error) setMode(Mode::ServerConnecting);
        dirty = true;
      }
      break;
    case WStype_TEXT:
      handleMessage(payload, len);
      break;
    default:
      break;
  }
}

// TLS certificates are checked against the current date, and the board boots
// thinking it's 1970. Get real time over NTP before opening wss://.
bool syncClock() {
  if (time(nullptr) > 1700000000) return true;
  drawStatus("Connecting", "WiFi: " + cfg.ssid, "Setting the clock...", "My IP: " + WiFi.localIP().toString());
  configTime(0, 0, "pool.ntp.org", "time.google.com", "time.cloudflare.com");
  uint32_t start = millis();
  while (time(nullptr) < 1700000000 && millis() - start < 15000) {
    improvLoop();
    delay(100);
  }
  return time(nullptr) > 1700000000;
}

void startWebSocket() {
  if (wsStarted) return;
  bool tls;
  String host, path;
  uint16_t port;
  if (!parseServerUrl(cfg.server, tls, host, port, path)) {
    errorText = "Bad server URL";
    setMode(Mode::Error);
    return;
  }
  if (tls && !syncClock()) {
    errorText = "No internet time (NTP)";
    Serial.println("makerDevice: NTP sync failed; TLS will likely fail");
  }
  if (tls)
    ws.beginSslWithCA(host.c_str(), port, path.c_str(), ROOT_CAS, "makerdevice");
  else
    ws.begin(host.c_str(), port, path.c_str(), "makerdevice");
  ws.onEvent(onWsEvent);
  ws.setReconnectInterval(4000);
  ws.enableHeartbeat(15000, 5000, 2);
  wsStarted = true;
  setMode(Mode::ServerConnecting);
}

void startWifi() {
  if (cfg.ssid.isEmpty()) {
    portalStart();
    setMode(Mode::Setup);
    return;
  }
  WiFi.mode(portalActive() ? WIFI_AP_STA : WIFI_STA);
  WiFi.setAutoReconnect(true);
  WiFi.begin(cfg.ssid.c_str(), cfg.pass.c_str());
  wifiStartedAt = millis();
  setMode(Mode::WifiConnecting);
}

bool onImprovWifi(const String& ssid, const String& pass) {
  drawStatus("Connecting to WiFi", ssid);
  WiFi.disconnect();
  WiFi.mode(portalActive() ? WIFI_AP_STA : WIFI_STA);
  WiFi.begin(ssid.c_str(), pass.c_str());
  uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < 20000) delay(100);
  if (WiFi.status() != WL_CONNECTED) {
    drawStatus("WiFi failed", "Could not join " + ssid, wifiReasonText(), "Board needs 2.4 GHz WiFi");
    delay(1500);
    startWifi();
    return false;
  }
  configSaveWifi(ssid, pass);
  portalStop();
  startWebSocket();
  return true;
}

void doOta(const String& url) {
  ws.disconnect();
  wsConnected = false;
  drawProgress("Updating firmware", 0);
  httpUpdate.onProgress([](int cur, int total) {
    static int last = -1;
    int pct = total ? (int)((int64_t)cur * 100 / total) : 0;
    if (pct != last) {
      last = pct;
      drawProgress("Updating firmware", pct);
    }
  });
  httpUpdate.rebootOnUpdate(true);
  t_httpUpdate_return r;
  if (url.startsWith("https://")) {
    WiFiClientSecure client;
    client.setCACert(ROOT_CAS);
    client.setTimeout(20);  // seconds; a stalled download must not hang the board
    r = httpUpdate.update(client, url);
  } else {
    WiFiClient client;
    client.setTimeout(20);
    r = httpUpdate.update(client, url);
  }
  (void)r;
  // Only reached on failure (success reboots). Restart cleanly on the current
  // firmware so the board reconnects instead of sitting offline.
  drawStatus("Update failed", httpUpdate.getLastErrorString(), "Restarting on the", "current firmware...");
  Serial.printf("makerDevice: OTA failed: %s\n", httpUpdate.getLastErrorString().c_str());
  delay(3000);
  ESP.restart();
}

// Keeps the game files on the board in line with the installed game apps:
// downloads new ones and new versions, deletes uninstalled ones.
void syncGames() {
  gamesSyncDue = false;
  gamesRetryAt = 0;
  if (state["apps"].isNull()) return;  // no app list yet: don't delete anything
  std::vector<GameRef> wanted;
  for (JsonObjectConst a : apps()) {
    for (JsonObjectConst s : a["screens"].as<JsonArrayConst>()) {
      if (isGameLayout(s["layout"] | "")) wanted.push_back({s["game"] | "", s["hash"] | ""});
    }
  }
  if (!gameStoreSync(wanted, configWebBase(), ROOT_CAS)) gamesRetryAt = millis() + 30000;
  dirty = true;
}

void onButton(int i) {
  lastInteraction = millis();
  if (mode != Mode::Ready) return;
  if (view == View::Drawer) {
    int n = appCount() + 1;  // the apps, then Settings
    drawerSel = constrain(drawerSel, 0, n - 1);
    if (i == 0)
      drawerSel = (drawerSel + n - 1) % n;
    else if (i == 2)
      drawerSel = (drawerSel + 1) % n;
    else if (drawerSel == n - 1)
      openSettings();
    else
      openAppAt(drawerSel);
  } else if (i == 1) {
    closeApp();
  } else if (view == View::Settings) {
    settingsPage = 1 - settingsPage;  // two pages: WiFi and Storage
    holdLeft = 0;
  } else if (view == View::App) {
    int n = appPages(currentApp()).size();
    if (n > 1) page = (page + (i == 2 ? 1 : n - 1)) % n;
    lastCycle = millis();
  }
  dirty = true;
}

// Holding D1 forgets WiFi from Settings' WiFi page, or from any status screen
// (where the drawer isn't available).
bool wifiResetAllowed() { return mode != Mode::Ready || (view == View::Settings && settingsPage == 0); }

// A game takes every press and release; holding the middle button leaves it.
bool inGame() { return mode == Mode::Ready && view == View::App && shownGame; }

void pollButtons() {
  uint32_t now = millis();
  for (int i = 0; i < 3; i++) {
    Button& b = buttons[i];
    bool pressed = digitalRead(b.pin) == (b.activeLow ? LOW : HIGH);
    if (pressed != b.down && now - b.changedAt > 30) {
      uint32_t held = now - b.changedAt;
      b.down = pressed;
      b.changedAt = now;
      b.longFired = false;
      // Upside down, D2 is the top button: swap so the top one always means up.
      int logical = cfg.flipped ? 2 - i : i;
      if (inGame()) {
        lastInteraction = now;
        gameButton(logical, pressed);
      } else if (i != 1) {
        if (pressed) onButton(logical);
      } else if (!pressed) {
        // D1 acts on release, so holding it for a WiFi reset doesn't also close Settings.
        if (held < SHORT_PRESS_MS) onButton(1);
        if (holdLeft) {
          holdLeft = 0;
          dirty = true;
        }
      }
    }
    if (i == 1 && b.down && !b.longFired && inGame() && now - b.changedAt >= SHORT_PRESS_MS) {
      b.longFired = true;
      closeApp();
      dirty = true;
    }
    if (i == 1 && b.down && !b.longFired && wifiResetAllowed()) {
      uint32_t held = now - b.changedAt;
      if (held >= WIFI_RESET_HOLD_MS) {
        b.longFired = true;
        drawStatus("WiFi reset", "Forgetting " + cfg.ssid, "Restarting...");
        configClearWifi();
        delay(1200);
        ESP.restart();
      } else if (held >= SHORT_PRESS_MS && mode == Mode::Ready) {
        int left = (WIFI_RESET_HOLD_MS - held + 999) / 1000;
        if (left != holdLeft) {
          holdLeft = left;
          dirty = true;
        }
      }
    }
  }
}

// Automatic page turns, and redraws for the clock, device fields and Settings.
void tick() {
  if (mode != Mode::Ready) return;
  uint32_t now = millis();
  if (view == View::App) {
    JsonObjectConst app = currentApp();
    uint32_t cycle = (uint32_t)(app["cycle"] | 0) * 1000;
    if (cycle && now - lastInteraction > cycle && now - lastCycle > cycle) {
      lastCycle = now;
      int n = appPages(app).size();
      if (n > 1) {
        page = (page + 1) % n;
        dirty = true;
      }
    }
  }
  if (view == View::Settings && now - lastSettingsDraw > 5000) dirty = true;
  if (shownLive && now - lastLiveDraw > 10000) dirty = true;
  if (shownClock) {
    time_t t = time(nullptr) + (long)(state["tz"] | 0);
    struct tm tmv;
    gmtime_r(&t, &tmv);
    if (tmv.tm_min != lastMinute) dirty = true;
  }
}

}  // namespace

void setup() {
  Serial.begin(115200);
  pinMode(buttons[0].pin, INPUT_PULLUP);
  pinMode(buttons[1].pin, INPUT_PULLDOWN);
  pinMode(buttons[2].pin, INPUT_PULLDOWN);
  displayInit();
  configLoad();
  displaySetOrientation(cfg.flipped, cfg.offsetX, cfg.offsetY);
  sensorsBegin();
  gameStoreBegin();
  drawStatus("Starting", "Firmware " FW_VERSION, "ID " + cfg.deviceId);
  WiFi.onEvent(
      [](WiFiEvent_t, WiFiEventInfo_t info) {
        wifiFailReason = info.wifi_sta_disconnected.reason;
        dirty = true;
      },
      ARDUINO_EVENT_WIFI_STA_DISCONNECTED);
  WiFi.onEvent([](WiFiEvent_t, WiFiEventInfo_t) { wifiFailReason = 0; }, ARDUINO_EVENT_WIFI_STA_GOT_IP);
  improvBegin(onImprovWifi);
  startWifi();
}

void loop() {
  improvLoop();
  portalLoop();
  pollButtons();

  if (mode == Mode::WifiConnecting) {
    if (WiFi.status() == WL_CONNECTED) {
      portalStop();
      startWebSocket();
    } else if (!portalActive() && millis() - wifiStartedAt > 30000) {
      // Saved network unreachable: also offer the hotspot so it can be changed.
      portalStart();
      WiFi.begin(cfg.ssid.c_str(), cfg.pass.c_str());
      dirty = true;
    }
  }
  if (wsStarted) ws.loop();

  if (!pendingOta.isEmpty()) {
    String url = pendingOta;
    pendingOta = "";
    doOta(url);
  }
  if (pendingReboot) ESP.restart();

  static bool slowShown = false;
  if (mode == Mode::ServerConnecting && !slowShown && millis() - serverSince > 20000) {
    slowShown = true;
    dirty = true;
  } else if (mode != Mode::ServerConnecting) {
    slowShown = false;
  }

  tick();
  if (shownGame) gameTick();
  if ((gamesSyncDue || (gamesRetryAt && millis() > gamesRetryAt)) && wsConnected) syncGames();
  if (toastUntil && millis() >= toastUntil) {
    toastUntil = 0;
    dirty = true;
  }
  if (dirty) {
    dirty = false;
    render();
  }
  delay(5);
}
