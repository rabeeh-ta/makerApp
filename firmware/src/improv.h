#pragma once
#include <Arduino.h>

// Improv WiFi over serial (https://www.improv-wifi.com/serial/). This is how
// the website's "Connect to WiFi" step hands credentials to the board right
// after flashing it over USB.

// Called with credentials from the browser. Returns true once WiFi is up.
typedef bool (*ImprovWifiHandler)(const String& ssid, const String& pass);

void improvBegin(ImprovWifiHandler handler);
void improvLoop();
