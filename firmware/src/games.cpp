// Runs the open Lua game at about 40 frames a second, and gives the game API
// (lua_api.h) its drawing, buttons, clock and best scores on this board.

#include "games.h"

#include <Adafruit_GFX.h>
#include <esp_random.h>
#include <time.h>

#include <map>

#include "config.h"
#include "display.h"
#include "game_store.h"
#include "game_vm.h"
#include "lua_api.h"

namespace {

constexpr int W = 240, H = 135;
constexpr uint32_t FRAME_MS = 25;
constexpr uint16_t BLACK = 0x0000, WHITE = 0xFFFF;
constexpr uint16_t HOG = displayRGB(0xF54E00), GRAY = displayRGB(0x8C8A85);

enum class Status { None, Waiting, Running, Failed };
Status status = Status::None;
GameRef current;
String title;
GameVM vm;
bool held[3];
uint32_t lastFrame = 0;

// The open app's saved values. Writes to flash are batched (at most one every
// two seconds) so an app saving every frame can't wear the flash out.
constexpr uint32_t VALUES_WRITE_MS = 2000;
constexpr size_t MAX_VALUES = 32;
std::map<String, String> values;
bool valuesDirty = false;
uint32_t valuesWrittenAt = 0;

void flushValues(bool now) {
  if (!valuesDirty || (!now && millis() - valuesWrittenAt < VALUES_WRITE_MS)) return;
  gameStoreSaveValues(current.id, values);
  valuesDirty = false;
  valuesWrittenAt = millis();
}

GFXcanvas16& canvas() { return *displayCanvas(); }

TextSize textSize(int size) { return size == 1 ? TextSize::Small : size == 2 ? TextSize::Bold : TextSize::Large; }

void load() {
  String source;
  if (!gameStoreRead(current, source)) return;  // still downloading
  status = vm.open(current.id.c_str(), source.c_str(), source.length()) ? Status::Running : Status::Failed;
  if (status == Status::Failed) Serial.printf("makerDevice: game %s: %s\n", current.id.c_str(), vm.error());
  lastFrame = millis();
}

// Splits an error message over two short card lines.
void errorCard() {
  String msg = vm.error();
  msg.replace("game:", "line ");
  String a = msg.substring(0, 26), b = msg.length() > 26 ? msg.substring(26, 50) : "";
  gamehost::card("App error", a.c_str(), b.c_str(), "Hold middle to leave");
}

}  // namespace

namespace gamehost {

void fillScreen(uint16_t c) { canvas().fillScreen(c); }
void fillRect(int x, int y, int w, int h, uint16_t c) { canvas().fillRect(x, y, w, h, c); }
void drawRect(int x, int y, int w, int h, uint16_t c) { canvas().drawRect(x, y, w, h, c); }
void fillRoundRect(int x, int y, int w, int h, int r, uint16_t c) { canvas().fillRoundRect(x, y, w, h, r, c); }
void fillCircle(int x, int y, int r, uint16_t c) { canvas().fillCircle(x, y, r, c); }
void drawCircle(int x, int y, int r, uint16_t c) { canvas().drawCircle(x, y, r, c); }
void drawLine(int x0, int y0, int x1, int y1, uint16_t c) { canvas().drawLine(x0, y0, x1, y1, c); }
void fillTriangle(int x0, int y0, int x1, int y1, int x2, int y2, uint16_t c) {
  canvas().fillTriangle(x0, y0, x1, y1, x2, y2, c);
}
void drawPixel(int x, int y, uint16_t c) { canvas().drawPixel(x, y, c); }

void text(int x, int y, const char* s, int size, uint16_t c) { displayText(x, y, s, textSize(size), c); }
void textCenter(int y, const char* s, int size, uint16_t c, int dx) { displayTextCenter(y, s, textSize(size), c, dx); }
int textWidth(const char* s, int size) { return displayTextWidth(s, textSize(size)); }

// The card games show before a round and after it: a title and up to three short lines.
void card(const char* title, const char* line1, const char* line2, const char* line3) {
  int lines = *line3 ? 3 : *line2 ? 2 : 1;
  int h = 40 + lines * 18;
  int top = (H - h) / 2;
  canvas().fillRoundRect(16, top, W - 32, h, 8, BLACK);
  canvas().drawRoundRect(16, top, W - 32, h, 8, HOG);
  displayTextCenter(top + 26, title, TextSize::Large, WHITE);
  const char* rows[] = {line1, line2, line3};
  for (int i = 0; i < lines; i++) displayTextCenter(top + 46 + i * 18, rows[i], TextSize::Small, GRAY);
}

// A small hedgehog facing right: spikes behind, a round body and an eye.
void hog(int x, int y, int r, uint16_t c) {
  for (int i = -1; i <= 1; i++) canvas().drawLine(x - r + 2, y + i * (r / 2 + 1), x - r - 3, y + i * (r / 2 + 2) - 1, BLACK);
  canvas().fillCircle(x, y, r, c);
  canvas().drawCircle(x, y, r, BLACK);
  canvas().fillCircle(x + r / 2, y - r / 3, max(1, r / 4), BLACK);
}

bool held(int b) { return ::held[b]; }
uint32_t millis() { return ::millis(); }
uint32_t unixTime() {
  time_t t = time(nullptr);
  return t > 1700000000 ? (uint32_t)t : 0;  // the board learns the time over the internet at boot
}
uint32_t random32() { return esp_random(); }
int loadBest(const char* game) { return configLoadInt((String(game) + "_best").c_str(), 0); }
void saveBest(const char* game, int score) { configSaveInt((String(game) + "_best").c_str(), score); }
void print(const char* s) { Serial.printf("makerDevice: app says: %s\n", s); }

const char* loadValue(const char*, const char* key) {
  auto it = values.find(key);
  return it == values.end() ? nullptr : it->second.c_str();
}

bool saveValue(const char*, const char* key, const char* value) {
  auto it = values.find(key);
  if (!value) {
    if (it == values.end()) return true;
    values.erase(it);
  } else if (it != values.end()) {
    if (it->second == value) return true;  // unchanged: nothing to write
    it->second = value;
  } else {
    if (values.size() >= MAX_VALUES) return false;
    values[key] = value;
  }
  valuesDirty = true;
  flushValues(false);
  return true;
}

}  // namespace gamehost

bool isGameLayout(const String& layout) { return layout == "game"; }

void gameOpen(const String& id, const String& hash, const String& name) {
  gameClose();
  current = {id, hash};
  title = name;
  gameStoreLoadValues(id, values);
  valuesDirty = false;
  held[0] = held[1] = held[2] = false;
  status = Status::Waiting;
  load();
}

void gameClose() {
  if (status != Status::None) flushValues(true);
  vm.close();  // frees the game's whole Lua state
  status = Status::None;
}

void gameDraw() {
  switch (status) {
    case Status::Running:
      if (!vm.draw()) status = Status::Failed;
      if (status == Status::Running) break;
      [[fallthrough]];
    case Status::Failed:
      canvas().fillScreen(BLACK);
      errorCard();
      break;
    case Status::Waiting:
      canvas().fillScreen(BLACK);
      gamehost::card(title.c_str(), gameStoreReady() ? "Downloading..." : "No room to store games", "",
                     "Hold middle to leave");
      break;
    case Status::None:
      return;
  }
  displayFlush();
}

void gameTick() {
  if (status == Status::Waiting && gameStoreHas(current)) {
    load();
    gameDraw();
    return;
  }
  flushValues(false);
  uint32_t now = millis();
  if (status != Status::Running || now - lastFrame < FRAME_MS) return;
  float dt = min(now - lastFrame, (uint32_t)50) / 1000.0f;  // after a stall, don't jump far
  lastFrame = now;
  if (!vm.update(dt)) status = Status::Failed;
  gameDraw();
}

void gameButton(int button, bool down) {
  if (button < 0 || button > 2) return;
  held[button] = down;
  if (status == Status::Running && !vm.button(button, down)) {
    status = Status::Failed;
    gameDraw();
  }
}
