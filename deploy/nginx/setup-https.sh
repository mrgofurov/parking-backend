#!/usr/bin/env bash
# One-time setup of the Smart Parking domains on the VPS's host nginx, with
# Let's Encrypt certificates. Other sites on the server are not touched.
#
#   sudo ./setup-https.sh [email]
#
# Domains (override with env vars if they change):
#   ADMIN_DOMAIN=adminpark.duckdns.org API_DOMAIN=apipark.duckdns.org CAPTURE_DOMAIN=capturepark.duckdns.org
set -euo pipefail

ADMIN_DOMAIN="${ADMIN_DOMAIN:-adminpark.duckdns.org}"
API_DOMAIN="${API_DOMAIN:-apipark.duckdns.org}"
CAPTURE_DOMAIN="${CAPTURE_DOMAIN:-capturepark.duckdns.org}"
EMAIL="${1:-}"
HERE="$(cd "$(dirname "$0")" && pwd)"
SITE=/etc/nginx/sites-available/smart-parking
HTPASSWD=/etc/nginx/smart-parking-capture.htpasswd

if [[ $EUID -ne 0 ]]; then
  echo "root kerak: sudo $0 ${EMAIL}" >&2
  exit 1
fi

echo "==> certbot va htpasswd o'rnatilmoqda"
apt-get update -qq
apt-get install -y -qq certbot python3-certbot-nginx apache2-utils >/dev/null

echo "==> nginx sayti: $SITE"
sed -e "s/ADMIN_DOMAIN/$ADMIN_DOMAIN/" \
    -e "s/API_DOMAIN/$API_DOMAIN/" \
    -e "s/CAPTURE_DOMAIN/$CAPTURE_DOMAIN/" \
    "$HERE/smart-parking.conf" > "$SITE"
ln -sf "$SITE" /etc/nginx/sites-enabled/smart-parking

if [[ ! -s "$HTPASSWD" ]]; then
  read -rp "Capture-service ($CAPTURE_DOMAIN) uchun login: " CAP_USER
  htpasswd -c "$HTPASSWD" "$CAP_USER" # asks for the password
  chown root:www-data "$HTPASSWD"
  chmod 640 "$HTPASSWD"
fi

nginx -t
systemctl reload nginx

if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  echo "==> ufw: 80 va 443 portlari ochilmoqda"
  ufw allow 'Nginx Full' >/dev/null
fi

echo "==> Let's Encrypt sertifikati: $ADMIN_DOMAIN, $API_DOMAIN, $CAPTURE_DOMAIN"
if [[ -n "$EMAIL" ]]; then
  ACCOUNT=(-m "$EMAIL")
else
  ACCOUNT=(--register-unsafely-without-email)
fi
certbot --nginx --non-interactive --agree-tos "${ACCOUNT[@]}" --redirect \
  -d "$ADMIN_DOMAIN" -d "$API_DOMAIN" -d "$CAPTURE_DOMAIN"

echo
echo "Tayyor:"
echo "  Admin panel:     https://$ADMIN_DOMAIN"
echo "  Backend API:     https://$API_DOMAIN/health"
echo "  Capture-service: https://$CAPTURE_DOMAIN/health (parol bilan)"
echo "Sertifikat avtomatik yangilanadi (certbot.timer)."
