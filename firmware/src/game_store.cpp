#include "game_store.h"

#include <HTTPClient.h>
#include <LittleFS.h>
#include <WiFiClientSecure.h>
#include <esp_flash.h>
#include <esp_partition.h>

namespace {

// After the last partition (hogcfg ends at 0x3E0000) up to the end of the 4 MB chip.
constexpr uint32_t STORE_OFFSET = 0x3E0000;
constexpr uint32_t STORE_SIZE = 0x20000;
constexpr size_t MAX_GAME_BYTES = 32 * 1024;
constexpr size_t MAX_ANIM_BYTES = 64 * 1024;
constexpr size_t SPARE_BYTES = 8 * 1024;  // LittleFS needs some free blocks to work in

bool mounted = false;
bool full = false;

String pathOf(const GameRef& g) { return "/" + g.id + "-" + g.hash + ".lua"; }
String valuesPath(const String& id) { return "/" + id + ".save"; }
String animPath(const String& hash) { return "/" + hash + ".anm"; }

// Saved values are stored one per line as key<TAB>value, with \\, \t and \n escaped.
String escape(const String& s) {
  String out;
  for (unsigned i = 0; i < s.length(); i++) {
    char c = s[i];
    if (c == '\\')
      out += "\\\\";
    else if (c == '\t')
      out += "\\t";
    else if (c == '\n')
      out += "\\n";
    else
      out += c;
  }
  return out;
}

String unescape(const String& s) {
  String out;
  for (unsigned i = 0; i < s.length(); i++) {
    if (s[i] != '\\' || i + 1 == s.length()) {
      out += s[i];
      continue;
    }
    char c = s[++i];
    out += c == 't' ? '\t' : c == 'n' ? '\n' : c;
  }
  return out;
}

bool download(const String& url, const String& path, size_t maxBytes, const char* ca) {
  WiFiClientSecure secure;
  WiFiClient plain;
  bool tls = url.startsWith("https://");
  if (tls) secure.setCACert(ca);
  HTTPClient http;
  http.setTimeout(15000);
  if (!http.begin(tls ? (WiFiClient&)secure : plain, url)) return false;
  int status = http.GET();
  int len = http.getSize();  // -1 when the reply is chunked (no length given)
  if (status != 200 || len == 0 || len > (int)maxBytes) {
    Serial.printf("makerDevice: %s download failed (%d, %d bytes)\n", path.c_str(), status, len);
    http.end();
    return false;
  }
  if (len > 0 && LittleFS.usedBytes() + len + SPARE_BYTES > LittleFS.totalBytes()) {
    Serial.printf("makerDevice: no room for %s (%d bytes)\n", path.c_str(), len);
    full = true;
    http.end();
    return false;
  }
  // Write to a temporary file first, so a cut-off download never looks installed.
  File f = LittleFS.open("/download.tmp", FILE_WRITE);
  if (!f) {
    http.end();
    return false;
  }
  int written = http.writeToStream(&f);
  f.close();
  http.end();
  bool complete = written > 0 && (size_t)written <= maxBytes && (len < 0 || written == len);
  if (!complete || !LittleFS.rename("/download.tmp", path)) {
    LittleFS.remove("/download.tmp");
    return false;
  }
  Serial.printf("makerDevice: installed %s (%d bytes)\n", path.c_str(), written);
  return true;
}

}  // namespace

bool gameStoreBegin() {
  const esp_partition_t* part = nullptr;
  esp_err_t err = esp_partition_register_external(esp_flash_default_chip, STORE_OFFSET, STORE_SIZE, "games",
                                                  ESP_PARTITION_TYPE_DATA, ESP_PARTITION_SUBTYPE_DATA_SPIFFS, &part);
  if (err != ESP_OK) {
    Serial.printf("makerDevice: game storage unavailable (%s)\n", esp_err_to_name(err));
    return false;
  }
  mounted = LittleFS.begin(true, "/games", 4, "games");  // formats it the first time
  return mounted;
}

bool gameStoreReady() { return mounted; }

bool gameStoreHas(const GameRef& g) { return mounted && LittleFS.exists(pathOf(g)); }

bool gameStoreRead(const GameRef& g, String& source) {
  if (!mounted) return false;
  File f = LittleFS.open(pathOf(g), FILE_READ);
  if (!f) return false;
  source = f.readString();
  f.close();
  return source.length() > 0;
}

bool gameStoreSync(const std::vector<GameRef>& wanted, const std::vector<String>& anims, const String& base,
                   const char* ca) {
  if (!mounted) return false;
  full = false;
  // Remove what's no longer installed, old versions and leftovers.
  std::vector<String> stale;
  File root = LittleFS.open("/");
  for (File f = root.openNextFile(); f; f = root.openNextFile()) {
    String name = "/" + String(f.name());
    bool keep = false;
    for (const GameRef& g : wanted) keep = keep || name == pathOf(g) || name == valuesPath(g.id);
    for (const String& hash : anims) keep = keep || name == animPath(hash);
    if (!keep) stale.push_back(name);
  }
  root.close();
  for (const String& name : stale) LittleFS.remove(name);

  bool ok = true;
  for (const GameRef& g : wanted) {
    String url = base + "/games/" + g.id + "/" + g.hash + ".lua";
    if (!LittleFS.exists(pathOf(g))) ok = download(url, pathOf(g), MAX_GAME_BYTES, ca) && ok;
  }
  for (const String& hash : anims) {
    if (!LittleFS.exists(animPath(hash))) ok = download(base + "/anims/" + hash + ".anm", animPath(hash), MAX_ANIM_BYTES, ca) && ok;
  }
  return ok;
}

bool gameStoreOpenAnim(const String& hash, File& file) {
  if (!mounted || hash.isEmpty() || !LittleFS.exists(animPath(hash))) return false;
  file = LittleFS.open(animPath(hash), FILE_READ);
  return (bool)file;
}

bool gameStoreFull() { return full; }

bool gameStoreLoadValues(const String& id, std::map<String, String>& values) {
  values.clear();
  if (!mounted) return false;
  File f = LittleFS.open(valuesPath(id), FILE_READ);
  if (!f) return true;  // nothing saved yet
  while (f.available()) {
    String line = f.readStringUntil('\n');
    int tab = line.indexOf('\t');
    if (tab > 0) values[unescape(line.substring(0, tab))] = unescape(line.substring(tab + 1));
  }
  f.close();
  return true;
}

bool gameStoreSaveValues(const String& id, const std::map<String, String>& values) {
  if (!mounted) return false;
  if (values.empty()) return LittleFS.remove(valuesPath(id)) || !LittleFS.exists(valuesPath(id));
  File f = LittleFS.open("/values.tmp", FILE_WRITE);
  if (!f) return false;
  for (const auto& kv : values) f.print(escape(kv.first) + "\t" + escape(kv.second) + "\n");
  f.close();
  LittleFS.remove(valuesPath(id));
  return LittleFS.rename("/values.tmp", valuesPath(id));
}

size_t gameStoreUsed() { return mounted ? LittleFS.usedBytes() : 0; }
size_t gameStoreTotal() { return mounted ? LittleFS.totalBytes() : STORE_SIZE; }
