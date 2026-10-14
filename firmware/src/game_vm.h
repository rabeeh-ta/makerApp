#pragma once
// One running Lua game: its own Lua state with a memory cap and a time limit
// per call into the game. close() frees everything, so a game that isn't open
// uses no memory. Plain C++ (no Arduino), shared with tools/gamecheck.

#include <stddef.h>
#include <stdint.h>

struct lua_State;
struct lua_Debug;

class GameVM {
 public:
  static constexpr size_t MEMORY_LIMIT = 160 * 1024;
  static constexpr uint32_t CALL_LIMIT_MS = 250;
  static constexpr uint32_t CALL_LIMIT_STEPS = 4000;  // thousands of Lua instructions per call

  ~GameVM() { close(); }

  // Runs the script (which defines start, update, draw and button) and calls start().
  bool open(const char* id, const char* source, size_t len);
  bool update(float dt);
  bool draw();
  bool button(int b, bool down);
  void close();

  bool ok() const { return L != nullptr && err[0] == 0; }
  const char* error() const { return err; }  // "" while the game is fine
  size_t peakMemory() const { return peak; }

 private:
  bool call(const char* fn, int nargs);
  void fail(const char* msg);
  static void* alloc(void* ud, void* ptr, size_t osize, size_t nsize);
  static void hook(lua_State* L, lua_Debug* ar);

  lua_State* L = nullptr;
  size_t used = 0, peak = 0;
  uint32_t callStart = 0, steps = 0;
  char err[160] = "";
};
