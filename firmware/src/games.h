#pragma once
#include <Arduino.h>

// Downloadable Lua games (internal/games). The launcher calls gameOpen
// when a game's app is opened, gameTick every loop while it's on screen, and
// forwards every press and release with gameButton. gameClose frees the game
// entirely, so a game that isn't open uses no memory. Buttons are logical
// (0 top, 1 middle, 2 bottom), already swapped for an upside-down board.

bool isGameLayout(const String& layout);
void gameOpen(const String& id, const String& hash, const String& name);
void gameClose();
void gameTick();  // loads a game once its download lands, advances and draws when a frame is due
void gameDraw();  // draws the current frame now, e.g. after the launcher drew over it
void gameButton(int button, bool down);
