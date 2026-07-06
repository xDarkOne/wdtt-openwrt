#!/bin/sh
# Publish the rotating VK call-token list over HTTPS so routers can pull it.
#
# The list is maintained by the existing whitelist-bypass headless creator at
# /opt/whitelist-bypass/active_call_vk.txt. We expose it read-only at an
# unguessable path by symlinking it into the nginx web root — no nginx config
# edits, and the symlink always reflects the live (rotated) file.
#
# Run on the WDTT server (root). Prints the hashes_url to put in the router's
# /etc/config/wdtt.
set -e

SRC="${WDTT_CALLS_SRC:-/opt/whitelist-bypass/active_call_vk.txt}"
[ -f "$SRC" ] || { echo "источник не найден: $SRC" >&2; exit 1; }

# Detect nginx web root from the running config; fall back to common defaults.
WEBROOT=$(nginx -T 2>/dev/null | grep -m1 -oE 'root[[:space:]]+[^;]+' | awk '{print $2}')
[ -n "$WEBROOT" ] || for d in /var/www/html /usr/share/nginx/html /var/www; do
	[ -d "$d" ] && WEBROOT="$d" && break
done
[ -n "$WEBROOT" ] || { echo "не удалось определить web root nginx" >&2; exit 1; }

# Reuse an existing token dir if this script was run before, else mint one.
EXISTING=$(find "$WEBROOT/wdtt" -maxdepth 2 -name vk-calls.txt 2>/dev/null | head -1 || true)
if [ -n "$EXISTING" ]; then
	LINK="$EXISTING"
	TOKEN=$(basename "$(dirname "$EXISTING")")
else
	TOKEN=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-24)
	mkdir -p "$WEBROOT/wdtt/$TOKEN"
	LINK="$WEBROOT/wdtt/$TOKEN/vk-calls.txt"
fi

ln -sf "$SRC" "$LINK"
chmod 0644 "$SRC"

# Public host: first global IPv4, or hostname.
HOST=$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)
[ -n "$HOST" ] || HOST=$(hostname -f 2>/dev/null || hostname)

echo "Опубликовано: $LINK -> $SRC"
echo
echo "hashes_url для роутера:"
echo "  https://$HOST/wdtt/$TOKEN/vk-calls.txt"
echo
echo "Проверка:"
echo "  curl -sk https://$HOST/wdtt/$TOKEN/vk-calls.txt | head"
