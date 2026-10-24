#pragma once
#include <Arduino.h>

// Fallback WiFi setup: the board opens its own hotspot ("makerDevice-XXXX") with a
// captive page for entering WiFi details from a phone. Used when the board
// has no WiFi saved or can't reach the saved network.

void portalStart();
void portalStop();
void portalLoop();
bool portalActive();
String portalSsid();
