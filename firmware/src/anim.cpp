#include "anim.h"

#include <Adafruit_GFX.h>
#include <LittleFS.h>

#include "display.h"
#include "game_store.h"

namespace {

constexpr int SCREEN_W = 240, SCREEN_H = 135;
constexpr uint8_t UNCHANGED = 15;
constexpr uint32_t MIN_FRAME_MS = 40;
constexpr size_t READ_BUF = 512;

File file;
uint8_t* pixels = nullptr;  // w*h color indexes
int w = 0, h = 0, frames = 0, frameNo = 0;
uint32_t firstFrameAt = 0;
uint32_t nextAt = 0;
uint16_t palette[16];

// Frame runs arrive a byte at a time; reading the file that way would be slow.
uint8_t buf[READ_BUF];
size_t bufLen = 0, bufPos = 0;

int nextByte() {
  if (bufPos == bufLen) {
    bufLen = file.read(buf, sizeof buf);
    bufPos = 0;
    if (bufLen == 0 || bufLen > sizeof buf) {
      bufLen = 0;
      return -1;
    }
  }
  return buf[bufPos++];
}

int nextU16() {
  int a = nextByte(), b = nextByte();
  return a < 0 || b < 0 ? -1 : a | b << 8;
}

void rewindFrames() {
  file.seek(firstFrameAt);
  bufLen = bufPos = 0;
  frameNo = 0;
}

// Applies the next frame to pixels. False if the file is damaged.
bool readFrame() {
  if (frameNo == frames) rewindFrames();
  int delayMs = nextU16(), size = nextU16();
  if (delayMs < 0 || size < 0) return false;
  int at = 0, total = w * h;
  for (int i = 0; i < size; i++) {
    int b = nextByte();
    if (b < 0) return false;
    uint8_t color = b >> 4;
    int n = (b & 15) + 1;
    if (n == 16) {
      int more = nextByte();
      if (more < 0) return false;
      n += more;
      i++;
    }
    if (at + n > total) return false;
    if (color != UNCHANGED) memset(pixels + at, color, n);
    at += n;
  }
  frameNo++;
  nextAt = millis() + max((uint32_t)delayMs, MIN_FRAME_MS);
  return true;
}

}  // namespace

bool animOpen(const String& hash) {
  animClose();
  if (!gameStoreOpenAnim(hash, file)) return false;
  uint8_t head[10];
  if (file.read(head, sizeof head) != sizeof head || memcmp(head, "MKA1", 4) != 0) {
    animClose();
    return false;
  }
  w = head[4];
  h = head[5];
  int colors = head[6];
  frames = head[8] | head[9] << 8;
  bool sane = w > 0 && w * 2 >= SCREEN_W && h > 0 && h * 2 >= SCREEN_H && colors > 0 && colors <= 15 && frames > 0;
  if (!sane || file.read((uint8_t*)palette, colors * 2) != (size_t)colors * 2) {
    animClose();
    return false;
  }
  pixels = (uint8_t*)calloc(w * h, 1);
  if (!pixels) {
    animClose();
    return false;
  }
  firstFrameAt = sizeof head + colors * 2;
  rewindFrames();
  if (!readFrame()) {
    animClose();
    return false;
  }
  return true;
}

void animClose() {
  if (file) file.close();
  free(pixels);
  pixels = nullptr;
  bufLen = bufPos = 0;
}

void animBlit() {
  if (!pixels) return;
  uint16_t* out = displayCanvas()->getBuffer();
  for (int y = 0; y < SCREEN_H; y++) {
    const uint8_t* row = pixels + (y / 2) * w;
    uint16_t* line = out + y * SCREEN_W;
    for (int x = 0; x < SCREEN_W; x++) line[x] = palette[row[x / 2]];
  }
}

bool animStep() {
  if (!pixels || frames < 2 || (int32_t)(millis() - nextAt) < 0) return false;
  if (readFrame()) return true;
  // Damaged: stop rather than draw garbage, and keep the picture that's up.
  frames = 1;
  return false;
}
