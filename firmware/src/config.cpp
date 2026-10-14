#include "config.h"

#include <ArduinoJson.h>
#include <Preferences.h>
#include <esp_mac.h>
#include <esp_partition.h>

HogConfig cfg;
static Preferences prefs;

static String randomHex(int bytes) {
  String s;
  for (int i = 0; i < bytes; i++) {
    char b[3];
    snprintf(b, sizeof b, "%02x", (uint8_t)esp_random());
    s += b;
  }
  return s;
}

// hogcfg layout: "HOGCFG1\0" | uint16 little-endian length | JSON {"server","claim"}
static void loadFlashConfig() {
  const esp_partition_t* p = esp_partition_find_first(
      ESP_PARTITION_TYPE_DATA, (esp_partition_subtype_t)0x40, "hogcfg");
  if (!p) return;
  static char buf[1024];
  if (esp_partition_read(p, 0, buf, sizeof buf) != ESP_OK) return;
  if (memcmp(buf, "HOGCFG1\0", 8) != 0) return;
  uint16_t len = (uint8_t)buf[8] | ((uint8_t)buf[9] << 8);
  if (len == 0 || len > sizeof buf - 10) return;
  JsonDocument doc;
  if (deserializeJson(doc, buf + 10, len)) return;
  cfg.server = doc["server"] | "";
  cfg.claim = doc["claim"] | "";
}

void configLoad() {
  uint8_t mac[6];
  esp_read_mac(mac, ESP_MAC_WIFI_STA);
  char id[13];
  snprintf(id, sizeof id, "%02X%02X%02X%02X%02X%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
  cfg.deviceId = id;

  loadFlashConfig();
  if (cfg.server.isEmpty()) cfg.server = DEFAULT_SERVER;

  prefs.begin("hog", false);
  cfg.ssid = prefs.getString("ssid", "");
  cfg.pass = prefs.getString("pass", "");
  cfg.token = prefs.getString("token", "");
  cfg.code = prefs.getString("code", "");
  if (cfg.token.isEmpty()) {
    cfg.token = randomHex(24);
    prefs.putString("token", cfg.token);
  }
  if (cfg.code.isEmpty()) configNewPairCode();
  cfg.flipped = prefs.getBool("flip", false);
  cfg.offsetX = prefs.getInt("ox", 0);
  cfg.offsetY = prefs.getInt("oy", 0);
  cfg.lastApp = prefs.getString("app", "");
}

void configSaveDisplay(bool flipped, int offsetX, int offsetY) {
  if (flipped == cfg.flipped && offsetX == cfg.offsetX && offsetY == cfg.offsetY) return;
  cfg.flipped = flipped;
  cfg.offsetX = offsetX;
  cfg.offsetY = offsetY;
  prefs.putBool("flip", flipped);
  prefs.putInt("ox", offsetX);
  prefs.putInt("oy", offsetY);
}

void configSaveLastApp(const String& id) {
  if (id == cfg.lastApp) return;
  cfg.lastApp = id;
  prefs.putString("app", id);
}

int configLoadInt(const char* key, int fallback) { return prefs.getInt(key, fallback); }

void configSaveInt(const char* key, int value) { prefs.putInt(key, value); }

void configSaveWifi(const String& ssid, const String& pass) {
  cfg.ssid = ssid;
  cfg.pass = pass;
  prefs.putString("ssid", ssid);
  prefs.putString("pass", pass);
}

void configClearWifi() {
  cfg.ssid = "";
  cfg.pass = "";
  prefs.remove("ssid");
  prefs.remove("pass");
}

void configNewPairCode() {
  char c[7];
  snprintf(c, sizeof c, "%06u", (unsigned)(esp_random() % 1000000));
  cfg.code = c;
  prefs.putString("code", cfg.code);
}

bool parseServerUrl(const String& url, bool& tls, String& host, uint16_t& port, String& path) {
  String rest;
  if (url.startsWith("wss://")) {
    tls = true;
    rest = url.substring(6);
  } else if (url.startsWith("ws://")) {
    tls = false;
    rest = url.substring(5);
  } else {
    return false;
  }
  int slash = rest.indexOf('/');
  String hostport = slash < 0 ? rest : rest.substring(0, slash);
  path = slash < 0 ? "/" : rest.substring(slash);
  int colon = hostport.indexOf(':');
  if (colon < 0) {
    host = hostport;
    port = tls ? 443 : 80;
  } else {
    host = hostport.substring(0, colon);
    port = hostport.substring(colon + 1).toInt();
  }
  return !host.isEmpty() && port > 0;
}

String configWebBase() {
  bool tls;
  String host, path;
  uint16_t port;
  if (!parseServerUrl(cfg.server, tls, host, port, path)) return "https://maker.rabeeta.xyz";
  String base = (tls ? "https://" : "http://") + host;
  if ((tls && port != 443) || (!tls && port != 80)) base += ":" + String(port);
  return base;
}

String configPairUrl() {
  return configWebBase() + "/pair?device=" + cfg.deviceId + "&code=" + cfg.code;
}
