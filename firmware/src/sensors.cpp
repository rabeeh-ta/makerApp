#include "sensors.h"

#include <Adafruit_MAX1704X.h>
#include <WiFi.h>
#include <Wire.h>
#include <time.h>

namespace {

Adafruit_MAX17048 fuelGauge;
bool hasGauge = false;
long tzOffset = 0;

String field(const String& name) {
  time_t now = time(nullptr) + tzOffset;
  struct tm t;
  gmtime_r(&now, &t);
  bool timeOk = now > 1700000000;
  char buf[24];
  if (name == "time" || name == "time12" || name == "date" || name == "day") {
    if (!timeOk) return "--";
    const char* fmt = name == "time" ? "%H:%M" : name == "time12" ? "%I:%M %p" : name == "date" ? "%a, %b %d" : "%A";
    strftime(buf, sizeof buf, fmt, &t);
    String out = buf;
    if (name == "time12" && out.startsWith("0")) out.remove(0, 1);
    return out;
  }
  if (name == "battery") return hasGauge ? String((int)round(constrain(fuelGauge.cellPercent(), 0.0f, 100.0f))) + "%" : "--";
  if (name == "battery_v") return hasGauge ? String(fuelGauge.cellVoltage(), 2) + "V" : "--";
  if (name == "chip_temp") return String((int)round(temperatureRead())) + "C";
  if (name == "wifi") return WiFi.isConnected() ? String(WiFi.RSSI()) + " dBm" : "off";
  if (name == "ip") return WiFi.isConnected() ? WiFi.localIP().toString() : "--";
  if (name == "uptime") {
    uint32_t m = millis() / 60000;
    if (m < 60) return String(m) + "m";
    if (m < 60 * 48) return String(m / 60) + "h " + String(m % 60) + "m";
    return String(m / 1440) + "d " + String((m / 60) % 24) + "h";
  }
  return "?";
}

}  // namespace

void sensorsBegin() {
  Wire.begin();
  hasGauge = fuelGauge.begin(&Wire);
}

void sensorsSetTimezone(long offsetSeconds) { tzOffset = offsetSeconds; }

String fillDeviceFields(const String& s) {
  int start = s.indexOf("{$");
  if (start < 0) return s;
  String out;
  int pos = 0;
  while (start >= 0) {
    int end = s.indexOf('}', start);
    if (end < 0) break;
    out += s.substring(pos, start);
    out += field(s.substring(start + 2, end));
    pos = end + 1;
    start = s.indexOf("{$", pos);
  }
  out += s.substring(pos);
  return out;
}
