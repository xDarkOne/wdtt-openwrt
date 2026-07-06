#!/bin/sh
# One-line installer for WDTT-OpenWrt. Downloads the repo tarball and hands off
# to scripts/install.sh. No git needed on the router; works on OpenWrt 24/25.
#
# Usage (replace YOURUSER, fill your server/token/password):
#
#   wget -qO- https://raw.githubusercontent.com/YOURUSER/wdtt-openwrt/main/bootstrap.sh \
#     | WDTT_SERVER=your-server WDTT_TOKEN=your-token WDTT_PASSWORD='owner-pass' \
#       sh -s -- --slot 0
#
# --slot N gives each router a non-overlapping set of VK calls (0, 1, 2, …).
set -e

# After you fork/create the repo, set this to <your-github-user>/wdtt-openwrt
# (or pass WDTT_REPO=... in the environment).
REPO="${WDTT_REPO:-YOURUSER/wdtt-openwrt}"
REF="${WDTT_REF:-main}"
TMP="/tmp/wdtt-openwrt-src"
URL="https://codeload.github.com/$REPO/tar.gz/refs/heads/$REF"

fetch() {
	if command -v uclient-fetch >/dev/null 2>&1; then uclient-fetch -qO "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
	elif command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
	else echo "нет wget/curl/uclient-fetch" >&2; exit 1; fi
}

case "$REPO" in
	YOURUSER/*) echo "Отредактируй REPO в bootstrap.sh (или задай WDTT_REPO=user/repo)"; exit 1 ;;
esac

echo "Скачиваю $REPO@$REF ..."
rm -rf "$TMP" /tmp/wdtt-src.tar.gz
mkdir -p "$TMP"
fetch "$URL" /tmp/wdtt-src.tar.gz
tar -xzf /tmp/wdtt-src.tar.gz -C "$TMP"

# The archive top dir is like wdtt-openwrt-main/ — find it without needing
# busybox tar's --strip-components.
INNER=""
for d in "$TMP"/*/; do INNER="$d"; break; done
[ -n "$INNER" ] && [ -f "${INNER}scripts/install.sh" ] || { echo "не нашёл scripts/install.sh в архиве" >&2; exit 1; }

exec sh "${INNER}scripts/install.sh" "$@"
