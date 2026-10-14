#pragma once
// Where downloaded games live: a small LittleFS file system in the last 128 KB
// of flash, which the partition table leaves unused. It's registered at boot,
// so boards get it with an ordinary WiFi update, no USB reflash. Each game is
// one file, /<id>-<hash>.lua; a new hash means a new version of the game.

#include <Arduino.h>

#include <map>
#include <vector>

struct GameRef {
  String id, hash;
};

bool gameStoreBegin();
bool gameStoreReady();
bool gameStoreHas(const GameRef& g);
bool gameStoreRead(const GameRef& g, String& source);

// Makes the store hold exactly these games: deletes the rest (uninstalled, or
// old versions, and saved values of uninstalled apps) and downloads what's
// missing from base + /games/<id>/<hash>.lua.
// ca is the root certificate list for https. Returns false if a download failed.
bool gameStoreSync(const std::vector<GameRef>& wanted, const String& base, const char* ca);

// The values an app saved with save(), kept in /<id>.save until the app is uninstalled.
bool gameStoreLoadValues(const String& id, std::map<String, String>& values);
bool gameStoreSaveValues(const String& id, const std::map<String, String>& values);

size_t gameStoreUsed();
size_t gameStoreTotal();
