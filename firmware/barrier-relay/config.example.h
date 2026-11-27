// Copy this file to config.h and fill it in. config.h is git-ignored because
// it holds the Wi-Fi password and the relay token.
#pragma once

// --- Wi-Fi -------------------------------------------------------------------
#define WIFI_SSID     "parking-wifi"
#define WIFI_PASSWORD "change-me"

// A fixed IP keeps the URLs in the admin panel valid after a reboot.
// Set USE_STATIC_IP to 0 to take an address from DHCP instead (then reserve it
// on the router).
#define USE_STATIC_IP 1
#define STATIC_IP     192, 168, 1, 120
#define GATEWAY_IP    192, 168, 1, 1
#define SUBNET_MASK   255, 255, 255, 0

// --- Security ----------------------------------------------------------------
// Every command must carry ?token=<RELAY_TOKEN>. Without it anyone on the
// Wi-Fi could open the barrier. Use a long random string.
#define RELAY_TOKEN "replace-with-a-long-random-string"

// --- Relays ------------------------------------------------------------------
// GPIO pins of the two relays. Common "ESP-12F 2-channel relay" boards use
// GPIO16 and GPIO14, but check the silkscreen or the seller's pinout: the
// board's relay LED should light when you test each channel.
#define RELAY_UP_PIN   16  // channel 1 -> controller UP + G   (open)
#define RELAY_DOWN_PIN 14  // channel 2 -> controller DOWN + G (close)

// 1 if a HIGH pin closes the relay (most of these boards), 0 if LOW does.
#define RELAY_ACTIVE_HIGH 1

// How long the contact stays closed. The barrier controller reacted to 1 s
// pulses in testing.
#define PULSE_MS 1000
