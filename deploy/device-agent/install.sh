#!/usr/bin/env bash
# Installs the device-agent on the parking's mini-PC (Ubuntu/Debian, systemd).
#
#   sudo ./install.sh ./parking-agent
#
# Build the binary first (on any machine, from the backend repo root):
#   GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=0.1.0" -o parking-agent ./cmd/device-agent
set -euo pipefail

BIN="${1:-./parking-agent}"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "root kerak: sudo $0 $BIN" >&2
  exit 1
fi
if [[ ! -x "$BIN" ]]; then
  echo "binar topilmadi: $BIN" >&2
  exit 1
fi

id parking-agent >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin parking-agent

install -m 0755 "$BIN" /usr/local/bin/parking-agent
install -d -m 0750 -o root -g parking-agent /etc/parking-agent
if [[ ! -f /etc/parking-agent/agent.env ]]; then
  install -m 0640 -o root -g parking-agent "$HERE/agent.env.example" /etc/parking-agent/agent.env
  echo "Sozlama yaratildi: /etc/parking-agent/agent.env — AGENT_SERVER_URL va AGENT_TOKEN ni kiriting."
fi
install -m 0644 "$HERE/parking-agent.service" /etc/systemd/system/parking-agent.service

systemctl daemon-reload
systemctl enable parking-agent >/dev/null

if grep -q '^AGENT_TOKEN=pa_xxxxxxxx' /etc/parking-agent/agent.env; then
  echo "Token hali kiritilmagan. Tahrirlang va ishga tushiring:"
  echo "  sudo nano /etc/parking-agent/agent.env"
  echo "  sudo systemctl restart parking-agent"
else
  systemctl restart parking-agent
  echo "Ishga tushdi. Holat:  systemctl status parking-agent   Log:  journalctl -u parking-agent -f"
fi
