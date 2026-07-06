#!/bin/sh
# Install the wdtt-linkd "fresh call-links" endpoint on the WDTT server (root).
#
# Serves up to 4 random current call links from the whitelist-bypass creator's
# rotating file, guarded by a random token. Prints the hashes_url for routers.
#
# Expects ./wdtt-linkd (the linux/amd64 binary) next to this script, or set
# WDTT_LINKD_BIN to its path.
set -e

BIN_SRC="${WDTT_LINKD_BIN:-$(dirname "$0")/wdtt-linkd}"
BIN_DST=/usr/local/bin/wdtt-linkd
ENV_FILE=/etc/wdtt-linkd.env
UNIT=/etc/systemd/system/wdtt-linkd.service
LISTEN="${LINKD_LISTEN:-0.0.0.0:56090}"
CALLS="${LINKD_FILE:-/opt/whitelist-bypass/active_call_vk.txt}"
PORT="${LISTEN##*:}"

[ "$(id -u)" = 0 ] || { echo "нужен root" >&2; exit 1; }
[ -f "$BIN_SRC" ] || { echo "не найден бинарник wdtt-linkd: $BIN_SRC" >&2; exit 1; }
[ -f "$CALLS" ]   || { echo "не найден файл ссылок: $CALLS" >&2; exit 1; }

install -m 0755 "$BIN_SRC" "$BIN_DST"

# Reuse an existing token so the router URL stays stable across re-runs.
if [ -f "$ENV_FILE" ]; then
	TOKEN=$(. "$ENV_FILE"; echo "$LINKD_TOKEN")
fi
[ -n "$TOKEN" ] || TOKEN=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-24)

cat > "$ENV_FILE" <<EOF
LINKD_LISTEN=$LISTEN
LINKD_FILE=$CALLS
LINKD_TOKEN=$TOKEN
EOF
chmod 0600 "$ENV_FILE"

install -m 0644 "$(dirname "$0")/wdtt-linkd.service" "$UNIT"

# Open the port (mirrors how wdtt-server manages its own rules).
if command -v iptables >/dev/null 2>&1; then
	iptables -C INPUT -p tcp --dport "$PORT" -m comment --comment WDTT_LINKD -j ACCEPT 2>/dev/null \
		|| iptables -I INPUT -p tcp --dport "$PORT" -m comment --comment WDTT_LINKD -j ACCEPT
fi

systemctl daemon-reload
systemctl enable --now wdtt-linkd

HOST=$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)
[ -n "$HOST" ] || HOST=$(hostname -f 2>/dev/null || hostname)

sleep 1
echo "----------------------------------------------------------------------"
echo "wdtt-linkd запущен на $LISTEN"
systemctl is-active wdtt-linkd >/dev/null 2>&1 && echo "статус: active" || echo "статус: НЕ активен (journalctl -u wdtt-linkd)"
echo
echo "hashes_url для роутера:"
echo "  http://$HOST:$PORT/$TOKEN/links?n=4"
echo
echo "Проверка:"
echo "  curl -s http://$HOST:$PORT/$TOKEN/links?n=4"
echo "----------------------------------------------------------------------"
