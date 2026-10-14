#pragma once
#include <Arduino.h>
#include <ArduinoJson.h>

#include <vector>

class GFXcanvas16;

// 240x135 landscape. Everything draws into an off-screen canvas, then
// displayFlush() pushes it in one go so screens never flicker.

void displayInit();
void displaySetBrightness(uint8_t pct);
void displayFlush();

// Mounting options: turn the picture 180° and/or shift it a few pixels
// (for cases or bezels that hide an edge).
void displaySetOrientation(bool flipped, int offsetX, int offsetY);

// While offline, screens show a small red dot in the top-right corner.
void displaySetOffline(bool offline);

void drawStatus(const String& title, const String& line1, const String& line2 = "",
                const String& line3 = "");
void drawPairing(const String& code, const String& host);
void drawProgress(const String& title, int pct);

// Long lists and texts are split into pages of five rows, so D0/D2 only ever
// step whole pages. screenPages says how many pages a screen takes.
int screenPages(JsonObjectConst s);

// Draws one page of a server-provided screen. index/count place the page dots.
void drawScreen(JsonObjectConst s, int part, int index, int count);

void drawToast(const String& text);

// Launcher building blocks: the built-in Clock, the Apps drawer and Settings.
void drawClock(const String& title, const String& hhmm, const String& date);
void drawMenu(const String& title, const std::vector<String>& items, int selected, const String& hint = "");
void drawSettings(const String& title, const std::vector<String>& rows, const String& footer, bool highlight,
                  int index, int count);

// A labelled usage bar, e.g. Firmware 1.0 / 1.9 MB. used runs from 0 to 1. Up to three fit.
struct Meter {
  String label;
  String value;
  float used;
};
void drawMeters(const String& title, const std::vector<Meter>& meters, int index, int count);

// For the built-in games: the off-screen canvas, colours from 0xRRGGBB, and
// text in the board's fonts. Call displayFlush() when the frame is drawn.
enum class TextSize { Small, Bold, Large };
constexpr uint16_t displayRGB(uint32_t hex) {
  return ((hex >> 8) & 0xF800) | ((hex >> 5) & 0x07E0) | ((hex >> 3) & 0x001F);
}
GFXcanvas16* displayCanvas();
void displayText(int x, int y, const String& s, TextSize size, uint16_t color);
void displayTextCenter(int y, const String& s, TextSize size, uint16_t color, int dx = 0);
int displayTextWidth(const String& s, TextSize size);
