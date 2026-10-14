#include "improv.h"

#include <WiFi.h>

#include "config.h"

namespace {

enum : uint8_t {
  TYPE_STATE = 0x01,
  TYPE_ERROR = 0x02,
  TYPE_RPC = 0x03,
  TYPE_RPC_RESULT = 0x04,
};
enum : uint8_t {
  STATE_READY = 0x02,
  STATE_PROVISIONING = 0x03,
  STATE_PROVISIONED = 0x04,
};
enum : uint8_t {
  ERR_NONE = 0x00,
  ERR_INVALID_RPC = 0x01,
  ERR_UNKNOWN_RPC = 0x02,
  ERR_UNABLE_TO_CONNECT = 0x03,
};
enum : uint8_t {
  CMD_WIFI_SETTINGS = 0x01,
  CMD_GET_STATE = 0x02,
  CMD_GET_INFO = 0x03,
  CMD_SCAN = 0x04,
};

ImprovWifiHandler wifiHandler = nullptr;
uint8_t buf[256];
size_t pos = 0;

void send(uint8_t type, const uint8_t* data, uint8_t len) {
  uint8_t hdr[9] = {'I', 'M', 'P', 'R', 'O', 'V', 1, type, len};
  uint8_t sum = 0;
  for (uint8_t b : hdr) sum += b;
  for (uint8_t i = 0; i < len; i++) sum += data[i];
  Serial.write(hdr, sizeof hdr);
  if (len) Serial.write(data, len);
  Serial.write(sum);
  Serial.write('\n');
  Serial.flush();
}

void sendState(uint8_t state) { send(TYPE_STATE, &state, 1); }
void sendError(uint8_t err) { send(TYPE_ERROR, &err, 1); }

void sendResult(uint8_t cmd, std::initializer_list<String> strings) {
  uint8_t out[250];
  size_t n = 2;
  for (const String& s : strings) {
    size_t l = min((size_t)s.length(), (size_t)120);
    if (n + 1 + l > sizeof out) break;
    out[n++] = l;
    memcpy(out + n, s.c_str(), l);
    n += l;
  }
  out[0] = cmd;
  out[1] = n - 2;
  send(TYPE_RPC_RESULT, out, n);
}

bool provisioned() { return WiFi.status() == WL_CONNECTED; }

void handleRpc(const uint8_t* data, uint8_t len) {
  if (len < 2 || data[1] + 2 > len) {
    sendError(ERR_INVALID_RPC);
    return;
  }
  uint8_t cmd = data[0];
  const uint8_t* body = data + 2;
  uint8_t bodyLen = data[1];

  switch (cmd) {
    case CMD_WIFI_SETTINGS: {
      if (bodyLen < 1 || body[0] + 1 > bodyLen) {
        sendError(ERR_INVALID_RPC);
        return;
      }
      uint8_t ssidLen = body[0];
      String ssid((const char*)body + 1, ssidLen);
      String pass;
      if (ssidLen + 1 < bodyLen) {
        uint8_t passLen = body[1 + ssidLen];
        pass = String((const char*)body + 2 + ssidLen, passLen);
      }
      sendError(ERR_NONE);
      sendState(STATE_PROVISIONING);
      if (wifiHandler && wifiHandler(ssid, pass)) {
        sendState(STATE_PROVISIONED);
        sendResult(CMD_WIFI_SETTINGS, {configPairUrl()});
      } else {
        sendError(ERR_UNABLE_TO_CONNECT);
        sendState(STATE_READY);
      }
      break;
    }
    case CMD_GET_STATE:
      if (provisioned()) {
        sendState(STATE_PROVISIONED);
        sendResult(CMD_GET_STATE, {configPairUrl()});
      } else {
        sendState(STATE_READY);
      }
      break;
    case CMD_GET_INFO:
      sendResult(CMD_GET_INFO, {"makerDevice", FW_VERSION, "ESP32-S3", "makerDevice-" + cfg.deviceId.substring(8)});
      break;
    case CMD_SCAN: {
      // The hotspot may already be scanning in the background; reuse or wait for it.
      int n = WiFi.scanComplete();
      uint32_t start = millis();
      while (n == WIFI_SCAN_RUNNING && millis() - start < 10000) {
        delay(50);
        n = WiFi.scanComplete();
      }
      if (n < 0) n = WiFi.scanNetworks();
      for (int i = 0; i < n && i < 20; i++) {
        if (WiFi.SSID(i).isEmpty()) continue;
        sendResult(CMD_SCAN, {WiFi.SSID(i), String(WiFi.RSSI(i)),
                              WiFi.encryptionType(i) == WIFI_AUTH_OPEN ? "NO" : "YES"});
      }
      WiFi.scanDelete();
      sendResult(CMD_SCAN, {});
      break;
    }
    default:
      sendError(ERR_UNKNOWN_RPC);
  }
}

// Feed one byte; dispatches a packet once the checksum byte arrives.
void feed(uint8_t b) {
  static const char HDR[] = "IMPROV";
  if (pos < 6) {
    if (b == (uint8_t)HDR[pos]) {
      buf[pos++] = b;
    } else {
      pos = (b == 'I') ? 1 : 0;
      if (pos) buf[0] = b;
    }
    return;
  }
  buf[pos++] = b;
  if (pos < 9) return;
  uint8_t len = buf[8];
  if (pos < 9u + len + 1) {
    if (pos >= sizeof buf) pos = 0;
    return;
  }
  uint8_t sum = 0;
  for (size_t i = 0; i < 9u + len; i++) sum += buf[i];
  uint8_t type = buf[7];
  pos = 0;
  if (sum != buf[9 + len]) return;
  if (type == TYPE_RPC) handleRpc(buf + 9, len);
}

}  // namespace

void improvBegin(ImprovWifiHandler handler) { wifiHandler = handler; }

void improvLoop() {
  while (Serial.available()) feed(Serial.read());
}
