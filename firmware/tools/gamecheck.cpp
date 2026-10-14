// gamecheck: runs downloadable game scripts on a computer, headless, against
// the same API (src/lua_api.cpp) and limits (src/game_vm.cpp) as the board.
// Each game plays 100 simulated seconds with random button presses, leaving and
// reopening halfway (so saved values get read back); any Lua
// error, hang or memory blow-up fails the check, and so does a local variable
// that hides an API name (a local TOP makes `b == TOP` quietly wrong). Run it
// with `make games-check`.
//
//   gamecheck path/to/snake.lua ...

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <map>
#include <random>
#include <sstream>
#include <string>

#include "game_vm.h"
#include "lua_api.h"

namespace {

uint32_t now = 0;  // simulated milliseconds
bool down[3];
std::map<std::string, int> bests;
std::map<std::string, std::string> saved;  // app/key -> value
std::mt19937 rng(42);

}  // namespace

namespace gamehost {

void fillScreen(uint16_t) {}
void fillRect(int, int, int, int, uint16_t) {}
void drawRect(int, int, int, int, uint16_t) {}
void fillRoundRect(int, int, int, int, int, uint16_t) {}
void fillCircle(int, int, int, uint16_t) {}
void drawCircle(int, int, int, uint16_t) {}
void drawLine(int, int, int, int, uint16_t) {}
void fillTriangle(int, int, int, int, int, int, uint16_t) {}
void drawPixel(int, int, uint16_t) {}
void text(int, int, const char*, int, uint16_t) {}
void textCenter(int, const char*, int, uint16_t, int) {}
int textWidth(const char* s, int size) { return (int)strlen(s) * (size == 3 ? 13 : 8); }
void card(const char*, const char*, const char*, const char*) {}
void hog(int, int, int, uint16_t) {}
bool held(int b) { return down[b]; }
uint32_t millis() { return now; }
uint32_t unixTime() { return 1790000000 + now / 1000; }
uint32_t random32() { return rng(); }
int loadBest(const char* game) { return bests[game]; }
void saveBest(const char* game, int score) { bests[game] = score; }
void print(const char* s) { printf("    print: %s\n", s); }
const char* loadValue(const char* app, const char* key) {
  auto it = saved.find(std::string(app) + "/" + key);
  return it == saved.end() ? nullptr : it->second.c_str();
}
bool saveValue(const char* app, const char* key, const char* value) {
  std::string k = std::string(app) + "/" + key;
  if (!value) {
    saved.erase(k);
    return true;
  }
  size_t count = 0;
  for (const auto& kv : saved) count += kv.first.compare(0, strlen(app) + 1, std::string(app) + "/") == 0;
  if (!saved.count(k) && count >= 32) return false;
  saved[k] = value;
  return true;
}

}  // namespace gamehost

// The game API's names (see lua_api.cpp). A local with one of these names hides it.
const char* const API_NAMES[] = {"W", "H", "TOP", "MIDDLE", "BOTTOM", "SMALL", "BOLD", "LARGE", "BLACK", "WHITE", "HOG",
                                 "GRAY", "DARK", "RED", "GREEN", "BLUE", "rgb", "cls", "rect", "rectb", "rrect", "circ",
                                 "circb", "line", "tri", "pix", "text", "ctext", "twidth", "shadow", "card", "hog", "held",
                                 "millis", "now", "best", "save", "saved", "print"};

// Describes the first local that reuses an API name, or returns "".
static std::string shadowedName(const std::string& src) {
  std::istringstream lines(src);
  std::string line;
  for (int n = 1; std::getline(lines, line); n++) {
    size_t at = line.find_first_not_of(" \t");
    if (at == std::string::npos || line.compare(at, 6, "local ") != 0) continue;
    std::string names = line.substr(at + 6);
    if (names.compare(0, 9, "function ") == 0) names = names.substr(9);
    names = names.substr(0, names.find_first_of("=-("));  // up to the value, a comment or a parameter list
    std::istringstream parts(names);
    for (std::string name; std::getline(parts, name, ',');) {
      name.erase(0, name.find_first_not_of(" \t"));
      name.erase(name.find_last_not_of(" \t") + 1);
      for (const char* api : API_NAMES)
        if (name == api) return "line " + std::to_string(n) + ": a local named " + name + " hides the API's " + name;
    }
  }
  return "";
}

static bool check(const char* path) {
  FILE* f = fopen(path, "rb");
  if (!f) {
    printf("FAIL %s: can't read it\n", path);
    return false;
  }
  std::string src;
  char buf[4096];
  for (size_t n; (n = fread(buf, 1, sizeof buf, f)) > 0;) src.append(buf, n);
  fclose(f);

  std::string id = path;
  id = id.substr(id.find_last_of('/') + 1);
  id = id.substr(0, id.find('.'));
  if (std::string why = shadowedName(src); !why.empty()) {
    printf("FAIL %-9s %s\n", id.c_str(), why.c_str());
    return false;
  }

  GameVM vm;
  now = 1000;
  down[0] = down[1] = down[2] = false;
  const int frames = 4000;  // 100 s at the board's 25 ms frames
  bool ok = vm.open(id.c_str(), src.data(), src.size());
  std::uniform_int_distribution<int> pct(0, 99), which(0, 2);
  for (int i = 0; ok && i < frames; i++) {
    now += 25;
    if (i == frames / 2) ok = vm.open(id.c_str(), src.data(), src.size());  // leave and come back
    if (pct(rng) < 6) {  // a press or release now and then, like a player would
      int b = which(rng);
      down[b] = !down[b];
      ok = vm.button(b, down[b]);
    }
    ok = ok && vm.update(0.025f) && vm.draw();
  }
  if (!ok) {
    printf("FAIL %-9s %s\n", id.c_str(), vm.error());
    return false;
  }
  printf("ok   %-9s %d frames, peak memory %zu KB\n", id.c_str(), frames, (vm.peakMemory() + 1023) / 1024);
  return true;
}

int main(int argc, char** argv) {
  int failed = 0;
  for (int i = 1; i < argc; i++) failed += !check(argv[i]);
  return failed ? 1 : 0;
}
