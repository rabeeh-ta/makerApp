#pragma once
#include <Arduino.h>

#ifndef FW_VERSION
#define FW_VERSION "dev"
#endif

#define DEFAULT_SERVER "wss://maker.app.rabeeta.xyz/ws"

// Everything the device remembers. WiFi, token and pairing code live in NVS;
// server URL and claim token come from the "hogcfg" partition that the website
// writes when it flashes the board.
struct HogConfig {
  String deviceId;  // 12 hex chars from the WiFi MAC
  String ssid;
  String pass;
  String server;    // ws://host:port/ws or wss://host/ws
  String claim;     // one-time token linking this device to the account that flashed it
  String token;     // device secret, generated on first boot
  String code;      // 6-digit pairing code shown while unclaimed
  bool flipped = false;  // display mounting, remembered so boot screens match
  int offsetX = 0;
  int offsetY = 0;
  String lastApp;  // app that was open, reopened after a restart ("" = the drawer)
};

extern HogConfig cfg;

void configLoad();
void configSaveWifi(const String& ssid, const String& pass);
void configClearWifi();
void configNewPairCode();
void configSaveDisplay(bool flipped, int offsetX, int offsetY);
void configSaveLastApp(const String& id);

// Small numbers that should survive a restart, like a game's best score.
// Keys are NVS keys: 15 characters at most.
int configLoadInt(const char* key, int fallback);
void configSaveInt(const char* key, int value);

// https://host[:port] derived from the WebSocket URL, for links shown to the user.
String configWebBase();
String configPairUrl();

bool parseServerUrl(const String& url, bool& tls, String& host, uint16_t& port, String& path);
