#include "portal.h"

#include <DNSServer.h>
#include <WebServer.h>
#include <WiFi.h>

#include "config.h"

namespace {

DNSServer dns;
WebServer server(80);
bool active = false;
String networksHtml;
uint32_t restartAt = 0;

String htmlEscape(const String& s) {
  String o;
  for (char c : s) {
    if (c == '<') o += "&lt;";
    else if (c == '>') o += "&gt;";
    else if (c == '&') o += "&amp;";
    else if (c == '"') o += "&quot;";
    else o += c;
  }
  return o;
}

const char PAGE_HEAD[] PROGMEM = R"(<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>makerDevice setup</title>
<style>body{font-family:system-ui,sans-serif;max-width:420px;margin:32px auto;padding:0 16px;background:#111;color:#eee}
h1{color:#F54E00;font-size:22px}label{display:block;margin:14px 0 6px;font-size:14px;color:#aaa}
input,select{width:100%;padding:12px;border-radius:8px;border:1px solid #333;background:#1b1b1b;color:#eee;font-size:16px;box-sizing:border-box}
button{margin-top:20px;width:100%;padding:14px;border:0;border-radius:8px;background:#F54E00;color:#fff;font-size:16px;font-weight:600}
p{color:#aaa;font-size:14px}</style></head><body><h1>makerDevice WiFi setup</h1>)";

void collectScan() {
  int n = WiFi.scanComplete();
  if (n == WIFI_SCAN_FAILED) WiFi.scanNetworks(true);  // results were consumed; refresh for next load
  if (n < 0) return;
  networksHtml = "";
  for (int i = 0; i < n && i < 25; i++) {
    String s = htmlEscape(WiFi.SSID(i));
    if (s.isEmpty() || networksHtml.indexOf("'" + s + "'") >= 0) continue;
    networksHtml += "<option value='" + s + "'>" + s + "</option>";
  }
  WiFi.scanDelete();
}

void handleRoot() {
  collectScan();
  String page = FPSTR(PAGE_HEAD);
  page += "<p>Pick the WiFi this board should use. It only works with <b>2.4 GHz</b> networks, "
          "so 5 GHz-only networks won't appear in the list.</p>"
          "<p>Using an iPhone hotspot? Turn on <b>Maximize Compatibility</b> in Personal Hotspot settings. "
          "Office networks that ask for a username or show a sign-in page won't work.</p>"
          "<form method='post' action='/save'>";
  page += "<label>Network</label><select onchange=\"document.getElementById('s').value=this.value\">";
  page += "<option value=''>Choose a network...</option>" + networksHtml + "</select>";
  page += "<label>Network name (SSID)</label><input id='s' name='ssid' required>";
  page += "<label>Password</label><input name='pass' type='password'>";
  page += "<button>Save and connect</button></form></body></html>";
  server.send(200, "text/html", page);
}

void handleSave() {
  String ssid = server.arg("ssid");
  if (ssid.isEmpty()) {
    server.sendHeader("Location", "/");
    server.send(303);
    return;
  }
  configSaveWifi(ssid, server.arg("pass"));
  String page = FPSTR(PAGE_HEAD);
  page += "<p>Saved. The board is restarting and will connect to <b>" + htmlEscape(ssid) +
          "</b>. You can close this page.</p></body></html>";
  server.send(200, "text/html", page);
  restartAt = millis() + 1500;
}

}  // namespace

String portalSsid() { return "makerDevice-" + cfg.deviceId.substring(8); }

bool portalActive() { return active; }

void portalStart() {
  if (active) return;
  WiFi.mode(WIFI_AP_STA);
  WiFi.softAP(portalSsid().c_str());
  dns.start(53, "*", WiFi.softAPIP());
  server.on("/", HTTP_GET, handleRoot);
  server.on("/save", HTTP_POST, handleSave);
  server.onNotFound([] {
    server.sendHeader("Location", "http://" + WiFi.softAPIP().toString() + "/");
    server.send(302);
  });
  server.begin();
  // Scan in the background so USB WiFi setup (Improv) stays responsive at boot.
  WiFi.scanNetworks(true);
  active = true;
}

void portalStop() {
  if (!active) return;
  server.stop();
  dns.stop();
  WiFi.softAPdisconnect(true);
  WiFi.mode(WIFI_STA);
  active = false;
}

void portalLoop() {
  if (!active) return;
  dns.processNextRequest();
  server.handleClient();
  if (restartAt && millis() > restartAt) ESP.restart();
}
