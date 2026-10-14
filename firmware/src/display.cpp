#include "display.h"

#include <Adafruit_GFX.h>
#include <Adafruit_ST7789.h>
#include <Fonts/FreeSans9pt7b.h>
#include <Fonts/FreeSansBold12pt7b.h>
#include <Fonts/FreeSansBold18pt7b.h>
#include <Fonts/FreeSansBold24pt7b.h>
#include <Fonts/FreeSansBold9pt7b.h>
#include <SPI.h>

#include <vector>

namespace {

constexpr int W = 240;
constexpr int H = 135;

constexpr uint16_t BLACK = 0x0000;
constexpr uint16_t WHITE = 0xFFFF;
constexpr uint16_t GRAY = 0x8C51;
constexpr uint16_t DARK = 0x2124;
constexpr uint16_t HOG = 0xFA60;  // #F54E00
constexpr int ROWS_PER_PAGE = 5;  // list rows / text lines on one page

Adafruit_ST7789 tft(TFT_CS, TFT_DC, TFT_RST);
GFXcanvas16* canvas = nullptr;
bool offline = false;
int offX = 0, offY = 0;

const GFXfont* const BIG_FONTS[] = {&FreeSansBold24pt7b, &FreeSansBold18pt7b, &FreeSansBold12pt7b,
                                    &FreeSansBold9pt7b};

uint16_t parseColor(const char* hex, uint16_t fallback) {
  if (!hex || hex[0] != '#' || strlen(hex) != 7) return fallback;
  uint32_t v = strtoul(hex + 1, nullptr, 16);
  return tft.color565((v >> 16) & 0xFF, (v >> 8) & 0xFF, v & 0xFF);
}

int textWidth(const String& s, const GFXfont* f) {
  canvas->setFont(f);
  int16_t x1, y1;
  uint16_t w, h;
  canvas->getTextBounds(s.c_str(), 0, 0, &x1, &y1, &w, &h);
  return w + x1;
}

String ellipsize(const String& s, const GFXfont* f, int maxW) {
  if (textWidth(s, f) <= maxW) return s;
  String t = s;
  while (t.length() > 1 && textWidth(t + "...", f) > maxW) t.remove(t.length() - 1);
  return t + "...";
}

void text(int x, int y, const String& s, const GFXfont* f, uint16_t color) {
  canvas->setFont(f);
  canvas->setTextColor(color);
  canvas->setCursor(x, y);
  canvas->print(s);
}

void textRight(int xr, int y, const String& s, const GFXfont* f, uint16_t color) {
  text(xr - textWidth(s, f), y, s, f, color);
}

void textCenter(int y, const String& s, const GFXfont* f, uint16_t color) {
  text((W - textWidth(s, f)) / 2, y, s, f, color);
}

// Largest bold font the string fits in.
const GFXfont* fitFont(const String& s, int maxW, int maxIndex = 3) {
  for (int i = 0; i <= maxIndex; i++) {
    if (textWidth(s, BIG_FONTS[i]) <= maxW) return BIG_FONTS[i];
  }
  return BIG_FONTS[maxIndex];
}

std::vector<String> wrap(const String& s, const GFXfont* f, int maxW) {
  std::vector<String> lines;
  int start = 0;
  while (start <= (int)s.length()) {
    int nl = s.indexOf('\n', start);
    String para = s.substring(start, nl < 0 ? s.length() : nl);
    String line;
    int i = 0;
    while (i <= (int)para.length()) {
      int sp = para.indexOf(' ', i);
      String word = para.substring(i, sp < 0 ? para.length() : sp);
      String trial = line.isEmpty() ? word : line + " " + word;
      if (textWidth(trial, f) > maxW && !line.isEmpty()) {
        lines.push_back(line);
        line = word;
      } else {
        line = trial;
      }
      if (sp < 0) break;
      i = sp + 1;
    }
    lines.push_back(line);
    if (nl < 0) break;
    start = nl + 1;
  }
  return lines;
}

void header(const String& title, uint16_t accent, int index, int count) {
  int dotsW = (count > 1 ? count * 9 : 0) + (offline ? 12 : 0);
  text(8, 19, ellipsize(title, &FreeSansBold9pt7b, W - 20 - dotsW), &FreeSansBold9pt7b, accent);
  int right = W - 8;
  if (offline) {
    canvas->fillCircle(right, 13, 3, 0xF8A2);  // red: not connected to the server
    right -= 12;
  }
  if (count > 1) {
    for (int i = 0; i < count && i < 12; i++) {
      int cx = right - (count - 1 - i) * 9;
      if (i == index)
        canvas->fillCircle(cx, 13, 3, accent);
      else
        canvas->drawCircle(cx, 13, 3, GRAY);
    }
  }
}

const GFXfont* fontFor(TextSize size) {
  switch (size) {
    case TextSize::Small:
      return &FreeSans9pt7b;
    case TextSize::Bold:
      return &FreeSansBold9pt7b;
    default:
      return &FreeSansBold12pt7b;
  }
}

std::vector<String> textLines(JsonObjectConst s) { return wrap(s["body"] | "", &FreeSans9pt7b, W - 16); }

void drawBigNumber(JsonObjectConst s) {
  String value = s["value"] | "-";
  String sub = s["sub"] | "";
  const GFXfont* f = fitFont(value, W - 16);
  text(8, sub.isEmpty() ? 92 : 82, value, f, WHITE);
  if (!sub.isEmpty()) {
    uint16_t c = parseColor(s["subColor"] | "", GRAY);
    text(8, 116, ellipsize(sub, &FreeSans9pt7b, W - 16), &FreeSans9pt7b, c);
  }
}

void drawText(JsonObjectConst s, int part) {
  auto lines = textLines(s);
  int first = part * ROWS_PER_PAGE;
  for (int i = 0; i < ROWS_PER_PAGE && first + i < (int)lines.size(); i++) {
    text(8, 46 + i * 19, lines[first + i], &FreeSans9pt7b, WHITE);
  }
}

void drawList(JsonObjectConst s, uint16_t accent, int part) {
  JsonArrayConst items = s["items"];
  const int rowH = 21, top = 30;
  int n = items.size();
  int first = part * ROWS_PER_PAGE;
  if (n == 0) text(8, 60, "No items", &FreeSans9pt7b, GRAY);
  for (int i = 0; i < ROWS_PER_PAGE && first + i < n; i++) {
    JsonArrayConst row = items[first + i];
    String label = row[0] | "";
    String value = row[1] | "";
    int y = top + i * rowH;
    int valueW = value.isEmpty() ? 0 : textWidth(value, &FreeSansBold9pt7b) + 10;
    text(8, y + 15, ellipsize(label, &FreeSans9pt7b, W - 20 - valueW), &FreeSans9pt7b, WHITE);
    if (!value.isEmpty()) textRight(W - 10, y + 15, value, &FreeSansBold9pt7b, accent);
    if (i < ROWS_PER_PAGE - 1 && first + i + 1 < n) canvas->drawFastHLine(8, y + rowH - 1, W - 18, DARK);
  }
}

void drawSpark(JsonObjectConst s, uint16_t accent) {
  String value = s["value"] | "";
  if (!value.isEmpty()) {
    const GFXfont* f = fitFont(value, W - 16);
    if (f == BIG_FONTS[0]) f = BIG_FONTS[1];  // leave room for the graph
    text(8, 54, value, f, WHITE);
  }
  JsonArrayConst series = s["series"];
  int n = series.size();
  if (n < 2) return;
  float lo = series[0].as<float>(), hi = lo;
  for (JsonVariantConst v : series) {
    lo = min(lo, v.as<float>());
    hi = max(hi, v.as<float>());
  }
  if (hi - lo < 1e-6f) hi = lo + 1;
  const int gx = 8, gy = 64, gw = W - 16, gh = H - gy - 6;
  int px = 0, py = 0;
  uint16_t fill = tft.color565(60, 30, 10);
  for (int i = 0; i < n; i++) {
    int x = gx + (long)i * gw / (n - 1);
    int y = gy + gh - (int)((series[i].as<float>() - lo) / (hi - lo) * gh);
    if (i > 0) {
      for (int fx = px; fx <= x; fx++) {
        int fy = py + (y - py) * (fx - px) / max(1, x - px);
        canvas->drawFastVLine(fx, fy, gy + gh - fy, fill);
      }
    }
    px = x;
    py = y;
  }
  for (int i = 1; i < n; i++) {
    int x0 = gx + (long)(i - 1) * gw / (n - 1);
    int x1 = gx + (long)i * gw / (n - 1);
    int y0 = gy + gh - (int)((series[i - 1].as<float>() - lo) / (hi - lo) * gh);
    int y1 = gy + gh - (int)((series[i].as<float>() - lo) / (hi - lo) * gh);
    canvas->drawLine(x0, y0, x1, y1, accent);
    canvas->drawLine(x0, y0 - 1, x1, y1 - 1, accent);
  }
}

void drawProgressLayout(JsonObjectConst s, uint16_t accent) {
  String value = s["value"] | "";
  float pct = constrain(s["pct"] | 0.0f, 0.0f, 1.0f);
  text(8, 74, value, fitFont(value, W - 16), WHITE);
  canvas->fillRoundRect(8, 88, W - 16, 14, 7, DARK);
  int fw = (int)((W - 16) * pct);
  if (fw > 0) canvas->fillRoundRect(8, 88, max(fw, 14), 14, 7, accent);
  String sub = s["sub"] | "";
  if (!sub.isEmpty()) text(8, 124, ellipsize(sub, &FreeSans9pt7b, W - 16), &FreeSans9pt7b, GRAY);
}

}  // namespace

void displayInit() {
  pinMode(TFT_I2C_POWER, OUTPUT);
  digitalWrite(TFT_I2C_POWER, HIGH);
  pinMode(TFT_BACKLITE, OUTPUT);
  digitalWrite(TFT_BACKLITE, HIGH);
  delay(10);
  tft.init(135, 240);
  tft.setRotation(3);
  tft.fillScreen(BLACK);
  canvas = new GFXcanvas16(W, H);
  canvas->setTextWrap(false);
}

void displaySetBrightness(uint8_t pct) {
  pct = constrain(pct, 5, 100);
  analogWrite(TFT_BACKLITE, pct * 255 / 100);
}

void displaySetOffline(bool v) { offline = v; }

void displaySetOrientation(bool flipped, int offsetX, int offsetY) {
  tft.setRotation(flipped ? 1 : 3);
  offX = constrain(offsetX, -20, 20);
  offY = constrain(offsetY, -20, 20);
  tft.fillScreen(BLACK);
}

void displayFlush() {
  // Blank the strips uncovered by the offset, then draw the shifted frame (clipped by the driver).
  if (offX > 0) tft.fillRect(0, 0, offX, H, BLACK);
  if (offX < 0) tft.fillRect(W + offX, 0, -offX, H, BLACK);
  if (offY > 0) tft.fillRect(0, 0, W, offY, BLACK);
  if (offY < 0) tft.fillRect(0, H + offY, W, -offY, BLACK);
  tft.drawRGBBitmap(offX, offY, canvas->getBuffer(), W, H);
}

void drawStatus(const String& title, const String& line1, const String& line2, const String& line3) {
  canvas->fillScreen(BLACK);
  text(8, 22, "makerDevice", &FreeSansBold12pt7b, HOG);
  text(8, 54, ellipsize(title, &FreeSansBold9pt7b, W - 16), &FreeSansBold9pt7b, WHITE);
  // Long lines wrap onto the next row instead of being cut off.
  std::vector<String> rows;
  for (const String* l : {&line1, &line2, &line3}) {
    if (l->isEmpty()) continue;
    for (const String& w : wrap(*l, &FreeSans9pt7b, W - 16)) rows.push_back(w);
  }
  const int ys[] = {80, 102, 124};
  for (int i = 0; i < 3 && i < (int)rows.size(); i++) {
    String r = rows[i];
    if (i == 2 && rows.size() > 3) r = ellipsize(r + " " + rows[3], &FreeSans9pt7b, W - 16);
    text(8, ys[i], r, &FreeSans9pt7b, GRAY);
  }
  displayFlush();
}

void drawPairing(const String& code, const String& host) {
  canvas->fillScreen(BLACK);
  text(8, 22, "Pair this device", &FreeSansBold9pt7b, HOG);
  String spaced = code.substring(0, 3) + " " + code.substring(3);
  textCenter(78, spaced, &FreeSansBold24pt7b, WHITE);
  textCenter(104, "Enter this code at", &FreeSans9pt7b, GRAY);
  textCenter(126, ellipsize(host + "/pair", &FreeSansBold9pt7b, W - 8), &FreeSansBold9pt7b, WHITE);
  displayFlush();
}

void drawProgress(const String& title, int pct) {
  canvas->fillScreen(BLACK);
  text(8, 22, "makerDevice", &FreeSansBold12pt7b, HOG);
  text(8, 60, title, &FreeSansBold9pt7b, WHITE);
  canvas->fillRoundRect(8, 80, W - 16, 14, 7, DARK);
  if (pct > 0) canvas->fillRoundRect(8, 80, max((W - 16) * pct / 100, 14), 14, 7, HOG);
  text(8, 120, String(pct) + "%", &FreeSans9pt7b, GRAY);
  displayFlush();
}

int screenPages(JsonObjectConst s) {
  String layout = s["layout"] | "";
  int rows = 0;
  if (layout == "list")
    rows = s["items"].size();
  else if (layout == "text")
    rows = textLines(s).size();
  return max(1, (rows + ROWS_PER_PAGE - 1) / ROWS_PER_PAGE);
}

void drawScreen(JsonObjectConst s, int part, int index, int count) {
  uint16_t accent = parseColor(s["accent"] | "", HOG);
  canvas->fillScreen(parseColor(s["bg"] | "", BLACK));
  header(s["title"] | "", accent, index, count);
  String layout = s["layout"] | "big_number";
  if (layout == "text")
    drawText(s, part);
  else if (layout == "list")
    drawList(s, accent, part);
  else if (layout == "spark")
    drawSpark(s, accent);
  else if (layout == "progress")
    drawProgressLayout(s, accent);
  else
    drawBigNumber(s);
  displayFlush();
}

void drawClock(const String& title, const String& hhmm, const String& date) {
  canvas->fillScreen(BLACK);
  if (!title.isEmpty()) text(8, 19, ellipsize(title, &FreeSansBold9pt7b, W - 30), &FreeSansBold9pt7b, HOG);
  if (offline) canvas->fillCircle(W - 8, 13, 3, 0xF8A2);
  textCenter(88, hhmm, &FreeSansBold24pt7b, WHITE);
  textCenter(120, date, &FreeSans9pt7b, GRAY);
  displayFlush();
}

void drawMenu(const String& title, const std::vector<String>& items, int selected, const String& hint) {
  canvas->fillScreen(BLACK);
  text(8, 19, ellipsize(title, &FreeSansBold9pt7b, W - 30), &FreeSansBold9pt7b, HOG);
  if (offline) canvas->fillCircle(W - 8, 13, 3, 0xF8A2);
  const int rowH = 21, top = 28;
  const int visible = hint.isEmpty() ? 5 : 4;
  int n = items.size();
  int first = constrain(selected - visible / 2, 0, max(0, n - visible));
  for (int i = 0; i < visible && first + i < n; i++) {
    int idx = first + i;
    int y = top + i * rowH;
    const String& label = items[idx];
    if (idx == selected) {
      canvas->fillRoundRect(4, y, W - 8, rowH - 1, 4, HOG);
      text(12, y + 15, ellipsize(label, &FreeSansBold9pt7b, W - 24), &FreeSansBold9pt7b, WHITE);
    } else {
      text(12, y + 15, ellipsize(label, &FreeSans9pt7b, W - 24), &FreeSans9pt7b, WHITE);
    }
  }
  if (!hint.isEmpty()) text(8, H - 8, ellipsize(hint, &FreeSans9pt7b, W - 16), &FreeSans9pt7b, GRAY);
  displayFlush();
}

void drawSettings(const String& title, const std::vector<String>& rows, const String& footer, bool highlight,
                  int index, int count) {
  canvas->fillScreen(BLACK);
  header(title, HOG, index, count);
  for (int i = 0; i < 4 && i < (int)rows.size(); i++) {
    text(8, 44 + i * 19, ellipsize(rows[i], &FreeSans9pt7b, W - 16), &FreeSans9pt7b, WHITE);
  }
  if (highlight) {
    canvas->fillRoundRect(4, H - 24, W - 8, 21, 4, HOG);
    text(12, H - 9, ellipsize(footer, &FreeSansBold9pt7b, W - 24), &FreeSansBold9pt7b, WHITE);
  } else {
    text(8, H - 9, ellipsize(footer, &FreeSans9pt7b, W - 16), &FreeSans9pt7b, GRAY);
  }
  displayFlush();
}

void drawMeters(const String& title, const std::vector<Meter>& meters, int index, int count) {
  canvas->fillScreen(BLACK);
  header(title, HOG, index, count);
  for (int i = 0; i < 3 && i < (int)meters.size(); i++) {
    const Meter& m = meters[i];
    int y = 46 + i * 32;
    text(8, y, m.label, &FreeSans9pt7b, WHITE);
    textRight(W - 8, y, m.value, &FreeSans9pt7b, GRAY);
    float used = constrain(m.used, 0.0f, 1.0f);
    canvas->fillRoundRect(8, y + 6, W - 16, 6, 3, DARK);
    int fill = (int)((W - 16) * used);
    if (fill > 0) canvas->fillRoundRect(8, y + 6, max(fill, 6), 6, 3, used > 0.85f ? 0xF8A2 : HOG);  // red when nearly full
  }
  displayFlush();
}

void drawToast(const String& msg) {
  String t = ellipsize(msg, &FreeSansBold9pt7b, W - 40);
  int w = textWidth(t, &FreeSansBold9pt7b) + 24;
  int x = (W - w) / 2;
  canvas->fillRoundRect(x, H - 34, w, 28, 14, WHITE);
  text(x + 12, H - 15, t, &FreeSansBold9pt7b, BLACK);
  displayFlush();
}

GFXcanvas16* displayCanvas() { return canvas; }

void displayText(int x, int y, const String& s, TextSize size, uint16_t color) { text(x, y, s, fontFor(size), color); }

void displayTextCenter(int y, const String& s, TextSize size, uint16_t color, int dx) {
  const GFXfont* f = fontFor(size);
  text((W - textWidth(s, f)) / 2 + dx, y, s, f, color);
}

int displayTextWidth(const String& s, TextSize size) { return textWidth(s, fontFor(size)); }
