#pragma once
#include <Arduino.h>

// The board's own data, shown on screens via {{ device.<name> }} templates.
// The server sends these as {$name} markers and the board fills them in, so a
// clock keeps ticking without the server.

void sensorsBegin();
void sensorsSetTimezone(long offsetSeconds);

// Replaces every {$name} marker in s with its current value.
String fillDeviceFields(const String& s);
