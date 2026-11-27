// Barrier relay: a 2-channel ESP8266 network relay for the parking backend.
//
// The backend's HTTP_RELAY barrier type sends
//   POST <relay_url>   to open   -> channel 1 pulses the controller's UP input
//   POST <close_url>   to close  -> channel 2 pulses the controller's DOWN input
//   HEAD <relay_url>   to check the connection ("Tekshirish" in the admin panel)
//
// Endpoints (all need ?token=RELAY_TOKEN except HEAD):
//   POST|GET /relay/1   pulse channel 1 (open)
//   POST|GET /relay/2   pulse channel 2 (close)
//   GET      /status    JSON with uptime, Wi-Fi signal and the last command
//
// The two channels are interlocked: a command first releases the other
// channel, so UP and DOWN are never closed at the same time.

#include <ESP8266WiFi.h>
#include <ESP8266WebServer.h>

#include "config.h"

ESP8266WebServer server(80);

struct Channel {
  uint8_t pin;
  const char *name;
  bool active;
  unsigned long startedAt;
};

Channel channels[2] = {
  {RELAY_UP_PIN, "UP", false, 0},
  {RELAY_DOWN_PIN, "DOWN", false, 0},
};

String lastCommand = "none";
unsigned long lastCommandAt = 0;

void writeRelay(Channel &ch, bool on) {
  bool level = RELAY_ACTIVE_HIGH ? on : !on;
  digitalWrite(ch.pin, level ? HIGH : LOW);
  ch.active = on;
  if (on) ch.startedAt = millis();
}

void startPulse(int index) {
  writeRelay(channels[1 - index], false);  // interlock
  writeRelay(channels[index], true);
  lastCommand = channels[index].name;
  lastCommandAt = millis();
  Serial.printf("[relay] %s pulse %d ms\n", channels[index].name, PULSE_MS);
}

// Releases a relay once its pulse is over. millis() subtraction stays correct
// across the ~49-day overflow.
void updatePulses() {
  for (Channel &ch : channels) {
    if (ch.active && millis() - ch.startedAt >= PULSE_MS) {
      writeRelay(ch, false);
    }
  }
}

bool authorized() {
  return server.hasArg("token") && server.arg("token") == RELAY_TOKEN;
}

void handleRelay(int index) {
  // HEAD is the backend's connection check: answer without touching relays.
  if (server.method() == HTTP_HEAD) {
    server.send(200);
    return;
  }
  if (!authorized()) {
    server.send(401, "application/json", "{\"ok\":false,\"error\":\"bad token\"}");
    return;
  }
  startPulse(index);
  server.send(200, "application/json",
              String("{\"ok\":true,\"channel\":\"") + channels[index].name + "\"}");
}

void handleStatus() {
  if (!authorized()) {
    server.send(401, "application/json", "{\"ok\":false,\"error\":\"bad token\"}");
    return;
  }
  String body = "{\"ok\":true";
  body += ",\"uptime_s\":" + String(millis() / 1000);
  body += ",\"rssi\":" + String(WiFi.RSSI());
  body += ",\"ip\":\"" + WiFi.localIP().toString() + "\"";
  body += ",\"last_command\":\"" + lastCommand + "\"";
  body += ",\"last_command_ago_s\":" + String(lastCommandAt ? (millis() - lastCommandAt) / 1000 : 0);
  body += "}";
  server.send(200, "application/json", body);
}

void connectWiFi() {
  WiFi.mode(WIFI_STA);
  WiFi.setAutoReconnect(true);
  WiFi.persistent(false);
#if USE_STATIC_IP
  WiFi.config(IPAddress(STATIC_IP), IPAddress(GATEWAY_IP), IPAddress(SUBNET_MASK), IPAddress(GATEWAY_IP));
#endif
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  Serial.printf("[wifi] connecting to %s", WIFI_SSID);
  unsigned long start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < 20000) {
    delay(250);
    Serial.print('.');
  }
  Serial.println();
  if (WiFi.status() == WL_CONNECTED) {
    Serial.printf("[wifi] connected, IP %s, RSSI %d dBm\n",
                  WiFi.localIP().toString().c_str(), WiFi.RSSI());
  } else {
    Serial.println("[wifi] not connected yet, will keep retrying");
  }
}

void setup() {
  // Relays off before anything else, so a reboot never pulses the barrier.
  for (Channel &ch : channels) {
    pinMode(ch.pin, OUTPUT);
    writeRelay(ch, false);
  }
  Serial.begin(115200);
  Serial.println("\n[boot] barrier relay");

  connectWiFi();

  server.on("/relay/1", [] { handleRelay(0); });
  server.on("/relay/2", [] { handleRelay(1); });
  server.on("/status", HTTP_GET, handleStatus);
  server.on("/", [] { server.send(200, "text/plain", "barrier relay"); });
  server.onNotFound([] { server.send(404, "text/plain", "not found"); });
  server.begin();
  Serial.println("[http] listening on port 80");
}

void loop() {
  server.handleClient();
  updatePulses();

  // If Wi-Fi drops for more than a minute, restart: the relays come back off.
  static unsigned long offlineSince = 0;
  if (WiFi.status() != WL_CONNECTED) {
    if (offlineSince == 0) offlineSince = millis();
    if (millis() - offlineSince > 60000) ESP.restart();
  } else {
    offlineSince = 0;
  }
}
