#pragma once
// The functions a downloadable Lua game can call (documented in
// internal/games/README.md). The same bindings run on the board and in
// tools/gamecheck on a computer; each side supplies the gamehost functions.

#include <stdint.h>

struct lua_State;

namespace gamehost {

void fillScreen(uint16_t c);
void fillRect(int x, int y, int w, int h, uint16_t c);
void drawRect(int x, int y, int w, int h, uint16_t c);
void fillRoundRect(int x, int y, int w, int h, int r, uint16_t c);
void fillCircle(int x, int y, int r, uint16_t c);
void drawCircle(int x, int y, int r, uint16_t c);
void drawLine(int x0, int y0, int x1, int y1, uint16_t c);
void fillTriangle(int x0, int y0, int x1, int y1, int x2, int y2, uint16_t c);
void drawPixel(int x, int y, uint16_t c);

// size: 1 small, 2 bold, 3 large. y is the text's baseline.
void text(int x, int y, const char* s, int size, uint16_t c);
void textCenter(int y, const char* s, int size, uint16_t c, int dx);
int textWidth(const char* s, int size);
void card(const char* title, const char* line1, const char* line2, const char* line3);
void hog(int x, int y, int r, uint16_t c);

bool held(int button);  // 0 top, 1 middle, 2 bottom
uint32_t millis();
uint32_t unixTime();  // seconds since 1970, or 0 while the board doesn't know the time
uint32_t random32();
int loadBest(const char* game);
void saveBest(const char* game, int score);
void print(const char* s);

// Small values an app keeps between visits and restarts, until it's
// uninstalled. Values are encoded by the bindings; nullptr removes the key.
// saveValue returns false when the app already has too many keys.
const char* loadValue(const char* app, const char* key);
bool saveValue(const char* app, const char* key, const char* value);

}  // namespace gamehost

// Opens the allowed standard libraries (base without file access, table,
// string, math, coroutine) and the game API in L. game is the game's ID, used
// for its best score.
void gameApiOpen(lua_State* L, const char* game);
