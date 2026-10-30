#pragma once
// Plays an animation (a converted GIF, internal/anim) from the app storage. It
// reads one frame at a time from flash and keeps the current picture as color
// indexes, so redraws and toasts never need to start it over.

#include <Arduino.h>

bool animOpen(const String& hash);  // false when it isn't downloaded (yet), or is damaged
void animClose();
void animBlit();  // draws the current frame on the canvas; flush it afterwards
bool animStep();  // moves on when the frame's time is up; true if the picture changed
