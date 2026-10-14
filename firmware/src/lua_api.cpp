// Lua bindings for downloadable games. Plain C++ with no Arduino headers, so
// tools/gamecheck can compile the very same file.

#include "lua_api.h"

#include <initializer_list>
#include <lua.hpp>
#include <math.h>
#include <stdio.h>
#include <string.h>

namespace {

constexpr int W = 240, H = 135;

int num(lua_State* L, int i) { return (int)luaL_checknumber(L, i); }

// Argument i as text (numbers too). Converts in place, so the stack doesn't
// grow and later optional arguments still read as missing.
const char* str(lua_State* L, int i) {
  luaL_checkany(L, i);
  luaL_tolstring(L, i, nullptr);
  lua_replace(L, i);
  return lua_tostring(L, i);
}
uint16_t color(lua_State* L, int i) { return (uint16_t)luaL_checkinteger(L, i); }

// Text sizes are 1 small, 2 bold, 3 large.
int size(lua_State* L, int i) {
  lua_Integer s = luaL_checkinteger(L, i);
  luaL_argcheck(L, s >= 1 && s <= 3, i, "text size must be SMALL, BOLD or LARGE");
  return (int)s;
}

int l_rgb(lua_State* L) {
  uint32_t hex = (uint32_t)luaL_checkinteger(L, 1);
  lua_pushinteger(L, ((hex >> 8) & 0xF800) | ((hex >> 5) & 0x07E0) | ((hex >> 3) & 0x001F));
  return 1;
}

int l_cls(lua_State* L) {
  gamehost::fillScreen(lua_isnoneornil(L, 1) ? 0 : color(L, 1));
  return 0;
}

int l_rect(lua_State* L) {
  gamehost::fillRect(num(L, 1), num(L, 2), num(L, 3), num(L, 4), color(L, 5));
  return 0;
}

int l_rectb(lua_State* L) {
  gamehost::drawRect(num(L, 1), num(L, 2), num(L, 3), num(L, 4), color(L, 5));
  return 0;
}

int l_rrect(lua_State* L) {
  gamehost::fillRoundRect(num(L, 1), num(L, 2), num(L, 3), num(L, 4), num(L, 5), color(L, 6));
  return 0;
}

int l_circ(lua_State* L) {
  gamehost::fillCircle(num(L, 1), num(L, 2), num(L, 3), color(L, 4));
  return 0;
}

int l_circb(lua_State* L) {
  gamehost::drawCircle(num(L, 1), num(L, 2), num(L, 3), color(L, 4));
  return 0;
}

int l_line(lua_State* L) {
  gamehost::drawLine(num(L, 1), num(L, 2), num(L, 3), num(L, 4), color(L, 5));
  return 0;
}

int l_tri(lua_State* L) {
  gamehost::fillTriangle(num(L, 1), num(L, 2), num(L, 3), num(L, 4), num(L, 5), num(L, 6), color(L, 7));
  return 0;
}

int l_pix(lua_State* L) {
  gamehost::drawPixel(num(L, 1), num(L, 2), color(L, 3));
  return 0;
}

int l_text(lua_State* L) {
  gamehost::text(num(L, 1), num(L, 2), str(L, 3), size(L, 4), color(L, 5));
  return 0;
}

int l_ctext(lua_State* L) {
  gamehost::textCenter(num(L, 1), str(L, 2), size(L, 3), color(L, 4), (int)luaL_optnumber(L, 5, 0));
  return 0;
}

int l_twidth(lua_State* L) {
  lua_pushinteger(L, gamehost::textWidth(str(L, 1), size(L, 2)));
  return 1;
}

// Centred text with a dark drop shadow.
int l_shadow(lua_State* L) {
  const char* s = str(L, 2);
  int y = num(L, 1), sz = size(L, 3);
  gamehost::textCenter(y + 1, s, sz, 0, 1);
  gamehost::textCenter(y, s, sz, color(L, 4), 0);
  return 0;
}

int l_card(lua_State* L) {
  gamehost::card(luaL_checkstring(L, 1), luaL_checkstring(L, 2), luaL_optstring(L, 3, ""), luaL_optstring(L, 4, ""));
  return 0;
}

int l_hog(lua_State* L) {
  gamehost::hog(num(L, 1), num(L, 2), num(L, 3), (uint16_t)luaL_optinteger(L, 4, 0xFA20));
  return 0;
}

int l_held(lua_State* L) {
  lua_Integer b = luaL_checkinteger(L, 1);
  lua_pushboolean(L, b >= 0 && b <= 2 && gamehost::held((int)b));
  return 1;
}

int l_millis(lua_State* L) {
  lua_pushinteger(L, (lua_Integer)(gamehost::millis() & 0x7FFFFFFF));
  return 1;
}

// best() returns the saved best score; best(score) keeps score if it beats it
// and returns the best either way.
int l_best(lua_State* L) {
  const char* game = lua_tostring(L, lua_upvalueindex(1));
  int best = gamehost::loadBest(game);
  if (!lua_isnoneornil(L, 1)) {
    int score = num(L, 1);
    if (score > best) {
      best = score;
      gamehost::saveBest(game, best);
    }
  }
  lua_pushinteger(L, best);
  return 1;
}

int l_now(lua_State* L) {
  lua_pushinteger(L, (lua_Integer)gamehost::unixTime());
  return 1;
}

// save(key, value) keeps a number, string or boolean for next time; nil forgets
// it. Stored as text with a type mark: #number, $string, !t / !f. (A plain
// buffer, not std::string: Lua errors jump past C++ destructors.)
int l_save(lua_State* L) {
  const char* app = lua_tostring(L, lua_upvalueindex(1));
  size_t keyLen;
  const char* key = luaL_checklstring(L, 1, &keyLen);
  luaL_argcheck(L, keyLen > 0 && keyLen <= 24, 1, "keys are 1 to 24 characters");
  char value[208];
  switch (lua_type(L, 2)) {
    case LUA_TNIL:
    case LUA_TNONE:
      gamehost::saveValue(app, key, nullptr);
      return 0;
    case LUA_TNUMBER:
      snprintf(value, sizeof value, "#%s", lua_tostring(L, 2));
      break;
    case LUA_TBOOLEAN:
      snprintf(value, sizeof value, "!%c", lua_toboolean(L, 2) ? 't' : 'f');
      break;
    case LUA_TSTRING: {
      size_t len;
      const char* s = lua_tolstring(L, 2, &len);
      luaL_argcheck(L, len <= 200 && strlen(s) == len, 2, "text up to 200 characters");
      snprintf(value, sizeof value, "$%s", s);
      break;
    }
    default:
      return luaL_argerror(L, 2, "save numbers, strings or booleans");
  }
  if (!gamehost::saveValue(app, key, value)) return luaL_error(L, "too many saved values (32 at most)");
  return 0;
}

// saved(key) returns what save(key, value) kept, or nil.
int l_saved(lua_State* L) {
  const char* v = gamehost::loadValue(lua_tostring(L, lua_upvalueindex(1)), luaL_checkstring(L, 1));
  if (!v || !*v) {
    lua_pushnil(L);
  } else if (v[0] == '#') {
    if (!lua_stringtonumber(L, v + 1)) lua_pushnil(L);
  } else if (v[0] == '!') {
    lua_pushboolean(L, v[1] == 't');
  } else {
    lua_pushstring(L, v + 1);
  }
  return 1;
}

int l_print(lua_State* L) {
  int n = lua_gettop(L);
  luaL_Buffer b;
  luaL_buffinit(L, &b);
  for (int i = 1; i <= n; i++) {
    if (i > 1) luaL_addchar(&b, ' ');
    luaL_tolstring(L, i, nullptr);
    luaL_addvalue(&b);
  }
  luaL_pushresult(&b);
  gamehost::print(lua_tostring(L, -1));
  return 0;
}

// math.random with the board's hardware random source instead of a fixed seed.
int l_random(lua_State* L) {
  double r = gamehost::random32() / 4294967296.0;  // [0, 1)
  switch (lua_gettop(L)) {
    case 0:
      lua_pushnumber(L, (lua_Number)r);
      return 1;
    case 1: {
      lua_Integer hi = luaL_checkinteger(L, 1);
      luaL_argcheck(L, hi >= 1, 1, "interval is empty");
      lua_pushinteger(L, 1 + (lua_Integer)(r * hi));
      return 1;
    }
    default: {
      lua_Integer lo = luaL_checkinteger(L, 1), hi = luaL_checkinteger(L, 2);
      luaL_argcheck(L, lo <= hi, 2, "interval is empty");
      lua_pushinteger(L, lo + (lua_Integer)(r * (hi - lo + 1)));
      return 1;
    }
  }
}

const luaL_Reg API[] = {
    {"rgb", l_rgb},     {"cls", l_cls},       {"rect", l_rect},     {"rectb", l_rectb}, {"rrect", l_rrect},
    {"circ", l_circ},   {"circb", l_circb},   {"line", l_line},     {"tri", l_tri},     {"pix", l_pix},
    {"text", l_text},   {"ctext", l_ctext},   {"twidth", l_twidth}, {"shadow", l_shadow}, {"card", l_card},
    {"hog", l_hog},     {"held", l_held},     {"millis", l_millis}, {"now", l_now},     {"print", l_print},
    {nullptr, nullptr},
};

void setInt(lua_State* L, const char* name, lua_Integer v) {
  lua_pushinteger(L, v);
  lua_setglobal(L, name);
}

}  // namespace

void gameApiOpen(lua_State* L, const char* game) {
  luaL_requiref(L, LUA_GNAME, luaopen_base, 1);
  luaL_requiref(L, LUA_TABLIBNAME, luaopen_table, 1);
  luaL_requiref(L, LUA_STRLIBNAME, luaopen_string, 1);
  luaL_requiref(L, LUA_MATHLIBNAME, luaopen_math, 1);
  luaL_requiref(L, LUA_COLIBNAME, luaopen_coroutine, 1);
  lua_settop(L, 0);

  // No file access, and only text chunks (no precompiled bytecode).
  for (const char* name : {"dofile", "loadfile", "load", "collectgarbage"}) {
    lua_pushnil(L);
    lua_setglobal(L, name);
  }
  lua_getglobal(L, LUA_MATHLIBNAME);
  lua_pushcfunction(L, l_random);
  lua_setfield(L, -2, "random");
  lua_pushnil(L);
  lua_setfield(L, -2, "randomseed");
  lua_pop(L, 1);

  lua_pushglobaltable(L);
  luaL_setfuncs(L, API, 0);
  lua_pop(L, 1);
  // best, save and saved know which app they belong to.
  const struct {
    const char* name;
    lua_CFunction fn;
  } perApp[] = {{"best", l_best}, {"save", l_save}, {"saved", l_saved}};
  for (const auto& f : perApp) {
    lua_pushstring(L, game);
    lua_pushcclosure(L, f.fn, 1);
    lua_setglobal(L, f.name);
  }

  setInt(L, "W", W);
  setInt(L, "H", H);
  setInt(L, "TOP", 0);
  setInt(L, "MIDDLE", 1);
  setInt(L, "BOTTOM", 2);
  setInt(L, "SMALL", 1);
  setInt(L, "BOLD", 2);
  setInt(L, "LARGE", 3);
  const struct {
    const char* name;
    uint32_t hex;
  } colors[] = {{"BLACK", 0x000000}, {"WHITE", 0xFFFFFF}, {"HOG", 0xF54E00}, {"GRAY", 0x8C8A85},
                {"DARK", 0x2A2A28},  {"RED", 0xF04438},   {"GREEN", 0x2BB673}, {"BLUE", 0x2D7FF9}};
  for (const auto& c : colors) {
    uint32_t h = c.hex;
    setInt(L, c.name, ((h >> 8) & 0xF800) | ((h >> 5) & 0x07E0) | ((h >> 3) & 0x001F));
  }
}
