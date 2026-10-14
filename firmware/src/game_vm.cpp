#include "game_vm.h"

#include <initializer_list>
#include <lua.hpp>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "lua_api.h"

void* GameVM::alloc(void* ud, void* ptr, size_t osize, size_t nsize) {
  GameVM* vm = static_cast<GameVM*>(ud);
  if (ptr == nullptr) osize = 0;  // for new blocks Lua passes the object type here
  if (nsize == 0) {
    free(ptr);
    vm->used -= osize;
    return nullptr;
  }
  if (nsize > osize && vm->used - osize + nsize > MEMORY_LIMIT) return nullptr;  // Lua reports "not enough memory"
  void* p = realloc(ptr, nsize);
  if (p) {
    vm->used = vm->used - osize + nsize;
    if (vm->used > vm->peak) vm->peak = vm->used;
  }
  return p;
}

// Runs every thousand instructions: stops a call that hangs or runs too long,
// before the board's watchdog would.
void GameVM::hook(lua_State* L, lua_Debug*) {
  GameVM* vm = *static_cast<GameVM**>(lua_getextraspace(L));
  if (++vm->steps > CALL_LIMIT_STEPS || gamehost::millis() - vm->callStart > CALL_LIMIT_MS)
    luaL_error(L, "took too long (an endless loop?)");
}

void GameVM::fail(const char* msg) {
  strncpy(err, msg && *msg ? msg : "unknown error", sizeof err - 1);
  err[sizeof err - 1] = 0;
}

bool GameVM::open(const char* id, const char* source, size_t len) {
  close();
  peak = 0;
  L = lua_newstate(alloc, this);
  if (!L) {
    fail("not enough memory to start");
    return false;
  }
  *static_cast<GameVM**>(lua_getextraspace(L)) = this;
  lua_sethook(L, hook, LUA_MASKCOUNT, 1000);
  gameApiOpen(L, id);

  callStart = gamehost::millis();
  steps = 0;
  if (luaL_loadbufferx(L, source, len, "=game", "t") != LUA_OK || lua_pcall(L, 0, 0, 0) != LUA_OK) {
    fail(lua_tostring(L, -1));
    return false;
  }
  for (const char* fn : {"start", "update", "draw"}) {
    if (lua_getglobal(L, fn) != LUA_TFUNCTION) {
      char msg[64];
      snprintf(msg, sizeof msg, "the game has no %s() function", fn);
      fail(msg);
      return false;
    }
    lua_pop(L, 1);
  }
  return call("start", 0);
}

bool GameVM::call(const char* fn, int nargs) {
  if (!ok()) return false;
  if (lua_getglobal(L, fn) != LUA_TFUNCTION) {  // button() is optional
    lua_pop(L, 1 + nargs);
    return true;
  }
  lua_insert(L, -1 - nargs);
  callStart = gamehost::millis();
  steps = 0;
  if (lua_pcall(L, nargs, 0, 0) != LUA_OK) {
    fail(lua_tostring(L, -1));
    lua_pop(L, 1);
    return false;
  }
  return true;
}

bool GameVM::update(float dt) {
  if (!ok()) return false;
  lua_pushnumber(L, dt);
  return call("update", 1);
}

bool GameVM::draw() { return call("draw", 0); }

bool GameVM::button(int b, bool down) {
  if (!ok()) return false;
  lua_pushinteger(L, b);
  lua_pushboolean(L, down);
  return call("button", 2);
}

void GameVM::close() {
  if (L) lua_close(L);
  L = nullptr;
  used = 0;
  err[0] = 0;
}
