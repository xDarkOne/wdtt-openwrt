#!/bin/sh
# WDTT-OpenWrt installer (selective / NetShift-style routing by default).
#
# Reads every file straight from the repo layout (openwrt/ + dist/), so just
# clone/extract the repo and run this. Works on OpenWrt 24.10 (opkg) and
# 25.x (apk) alike — the package manager is auto-detected.
#
# Quick per-router install (secrets passed at run time, never stored in git):
#
#   WDTT_SERVER=your-server \
#   WDTT_TOKEN=your-linkd-token \
#   WDTT_PASSWORD='owner-password' \
#   sh scripts/install.sh --slot 0
#
# --slot N (or WDTT_SLOT) reserves a non-overlapping set of VK calls per router.
#
# Advanced overrides: WDTT_PEER (host:port), WDTT_HASHES_URL (full URL),
# WDTT_PORT (DTLS, default 56000), WDTT_LINKD_PORT (default 56090),
# WDTT_N (calls per router, default 4), WDTT_MODE, WDTT_DOMAINS_URL,
# WDTT_BASE_URL (fetch the binary from a URL instead of dist/).
set -e

SELF_DIR=$(cd "$(dirname "$0")" && pwd)
# Repo root = the dir that actually holds openwrt/ + dist/ (this script may live
# in scripts/, or be called from the root by bootstrap.sh).
if [ -d "$SELF_DIR/openwrt" ] && [ -d "$SELF_DIR/dist" ]; then
	REPO_ROOT="$SELF_DIR"
else
	REPO_ROOT=$(cd "$SELF_DIR/.." && pwd)
fi
OPENWRT="$REPO_ROOT/openwrt"

BIN_DST=/usr/sbin/wdtt-client
INIT_DST=/etc/init.d/wdtt-client
CFG_DST=/etc/config/wdtt
NFT_DST=/etc/nftables.d/10-wdtt.nft
NFSET=wdtt_dst4
DOMAINS_URL="${WDTT_DOMAINS_URL:-https://raw.githubusercontent.com/itdoginfo/allow-domains/main/Russia/inside-dnsmasq-nfset.lst}"

say() { printf '\n\033[1m%s\033[0m\n' "$1"; }
err() { printf '\033[31m[ОШИБКА]\033[0m %s\n' "$1" >&2; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$1"; }

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--slot) WDTT_SLOT="$2"; shift 2 ;;
			--slot=*) WDTT_SLOT="${1#*=}"; shift ;;
			--server) WDTT_SERVER="$2"; shift 2 ;;
			--token) WDTT_TOKEN="$2"; shift 2 ;;
			*) warn "неизвестный аргумент: $1"; shift ;;
		esac
	done
}

# Turn the friendly WDTT_SERVER/WDTT_TOKEN/WDTT_SLOT into the peer + hashes_url
# the daemon actually needs, unless the caller gave the full values directly.
derive_secrets() {
	port="${WDTT_PORT:-56000}"
	lport="${WDTT_LINKD_PORT:-56090}"
	n="${WDTT_N:-4}"
	if [ -z "$WDTT_PEER" ] && [ -n "$WDTT_SERVER" ]; then
		case "$WDTT_SERVER" in
			*:*) WDTT_PEER="$WDTT_SERVER" ;;      # already host:port
			*)   WDTT_PEER="$WDTT_SERVER:$port" ;;
		esac
	fi
	if [ -z "$WDTT_HASHES_URL" ] && [ -n "$WDTT_SERVER" ] && [ -n "$WDTT_TOKEN" ]; then
		host="${WDTT_SERVER%%:*}"
		WDTT_HASHES_URL="http://$host:$lport/$WDTT_TOKEN/links?n=$n"
	fi
	# Append the slot so multiple routers never share calls.
	if [ -n "$WDTT_SLOT" ] && [ -n "$WDTT_HASHES_URL" ]; then
		case "$WDTT_HASHES_URL" in
			*slot=*) : ;;
			*\?*) WDTT_HASHES_URL="$WDTT_HASHES_URL&slot=$WDTT_SLOT" ;;
			*)    WDTT_HASHES_URL="$WDTT_HASHES_URL?slot=$WDTT_SLOT" ;;
		esac
	fi
}

detect_asset() {
	case "$(uname -m)" in
		aarch64|arm64) echo "wdtt-client-linux-arm64" ;;
		armv7l|armv7)  echo "wdtt-client-linux-armv7" ;;
		mipsel|mipsle) echo "wdtt-client-linux-mipsle" ;;
		mips)          echo "wdtt-client-linux-mips" ;;
		x86_64|amd64)  echo "wdtt-client-linux-amd64" ;;
		*) echo "" ;;
	esac
}

fetch() {
	if command -v uclient-fetch >/dev/null 2>&1; then uclient-fetch -qO "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
	elif command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
	else err "нет uclient-fetch/wget/curl"; return 1; fi
}

pkg_install() {
	if command -v apk >/dev/null 2>&1; then apk add "$@"; else opkg install "$@"; fi
}

ensure_deps() {
	say "[1/7] Зависимости (wireguard-tools, kmod-wireguard)..."
	command -v apk >/dev/null 2>&1 && apk update >/dev/null 2>&1 || opkg update >/dev/null 2>&1 || true
	if ! command -v wg >/dev/null 2>&1; then
		pkg_install wireguard-tools kmod-wireguard || { err "не удалось поставить wireguard-tools"; return 1; }
	fi
	modprobe wireguard 2>/dev/null || true
	ip rule show >/dev/null 2>&1 || pkg_install ip-full || warn "ip rule недоступен — нужен ip-full"
	echo "-> wg + ip готовы"
}

# Selective routing no longer needs dnsmasq-full: wdtt-resolve fills the nft
# sets by resolving the domain lists itself. So we NEVER swap dnsmasq (that swap
# is the one step that can strand a remote router). We just check a resolver is
# reachable so wdtt-resolve will work.
check_resolver() {
	say "[2/7] Проверка резолвера (dnsmasq НЕ трогаем)..."
	if ! command -v nslookup >/dev/null 2>&1; then
		warn "nslookup недоступен — wdtt-resolve не сможет резолвить домены (поставь busybox nslookup или используй списки подсетей)"
		return 0
	fi
	if nslookup openwrt.org >/dev/null 2>&1; then
		echo "-> DNS резолвит, wdtt-resolve будет наполнять сеты"
	else
		warn "DNS сейчас не резолвит — сеты наполнятся, когда резолвер заработает (или через списки подсетей)"
	fi
}

install_files() {
	say "[3/7] Бинарник и служба..."
	asset=$(detect_asset)
	if [ -n "$asset" ] && [ -f "$REPO_ROOT/dist/$asset" ]; then
		cp "$REPO_ROOT/dist/$asset" "$BIN_DST"
	elif [ -n "$WDTT_BASE_URL" ] && [ -n "$asset" ]; then
		echo "-> загрузка $asset"; fetch "$WDTT_BASE_URL/$asset" "$BIN_DST"
	else
		err "нет dist/$asset (арх $(uname -m)) и не задан WDTT_BASE_URL"; return 1
	fi
	chmod 0755 "$BIN_DST"

	cp "$OPENWRT/etc/init.d/wdtt-client" "$INIT_DST"
	chmod 0755 "$INIT_DST"
	echo "-> $BIN_DST, $INIT_DST"
}

gen_device_id() {
	mac=$(cat /sys/class/net/br-lan/address 2>/dev/null)
	[ -z "$mac" ] && mac=$(cat /sys/class/net/eth0/address 2>/dev/null)
	printf '%s%s' "$mac" "$(cat /proc/sys/kernel/hostname 2>/dev/null)" | md5sum | cut -c1-16
}

install_config() {
	say "[4/7] Конфиг /etc/config/wdtt..."
	if [ ! -f "$CFG_DST" ]; then
		cp "$OPENWRT/etc/config/wdtt" "$CFG_DST"
		echo "-> создан конфиг"
	else echo "-> конфиг уже есть, не трогаю опции (обновлю только заданные)"; fi

	dev=$(uci -q get wdtt.settings.device_id || echo "")
	[ -z "$dev" ] && { dev=$(gen_device_id); uci set wdtt.settings.device_id="$dev"; echo "-> device_id=$dev"; }

	[ -n "$WDTT_PEER" ]       && uci set wdtt.settings.peer="$WDTT_PEER"
	[ -n "$WDTT_PASSWORD" ]   && uci set wdtt.settings.password="$WDTT_PASSWORD"
	[ -n "$WDTT_HASHES_URL" ] && uci set wdtt.settings.hashes_url="$WDTT_HASHES_URL"
	[ -n "$WDTT_MODE" ]       && uci set wdtt.settings.mode="$WDTT_MODE"
	uci commit wdtt
	chmod 0600 "$CFG_DST"
	[ -n "$WDTT_SLOT" ] && echo "-> slot=$WDTT_SLOT"
	echo "-> peer=$(uci -q get wdtt.settings.peer), hashes_url=$(uci -q get wdtt.settings.hashes_url)"
}

setup_nft_and_domains() {
	say "[5/7] nftables-сеты + разметка + генерация списков..."
	# fw4 include: dst set (filled by wdtt-resolve from domains + static subnets),
	# src set (fully routed devices). Prerouting rule marks matching src/dst.
	# Sets live in 05-wdtt-sets.nft (wdtt-resolve rewrites it with inline elements,
	# which — unlike runtime `add element` — survive fw4 reloads). Chains that
	# reference them go in 10-wdtt.nft (05 sorts first, so sets load first).
	cat > /etc/nftables.d/05-wdtt-sets.nft <<EOF
set $NFSET { type ipv4_addr; flags interval; auto-merge; }
set wdtt_dst6 { type ipv6_addr; flags interval; auto-merge; }
set wdtt_src4 { type ipv4_addr; flags interval; auto-merge; }
set wdtt_doh4 { type ipv4_addr; flags interval; auto-merge; }
set wdtt_doh6 { type ipv6_addr; flags interval; auto-merge; }
EOF
	rm -f /etc/nftables.d/11-wdtt-elements.nft 2>/dev/null
	cat > "$NFT_DST" <<EOF
chain wdtt_mark {
	type filter hook prerouting priority mangle; policy accept;
	# OR the bit in (don't clobber marks from zapret 0x40000000 / NetShift
	# 0x100000). On overlap NetShift's lower-pref ip rule wins, so WDTT can
	# never break it.
	ip saddr @wdtt_src4 meta mark set meta mark or 0x1
	ip daddr @$NFSET meta mark set meta mark or 0x1
}
chain wdtt_filter {
	type filter hook forward priority -150; policy accept;
	# All rules are gated by set population (empty set = no-op), so config
	# toggles just control whether wdtt-resolve fills the sets.
	# DoH/DoT: force LAN clients onto the router's resolver.
	ip daddr @wdtt_doh4 tcp dport { 443, 853 } reject with tcp reset
	ip daddr @wdtt_doh4 udp dport { 443, 853 } drop
	ip6 daddr @wdtt_doh6 tcp dport { 443, 853 } reject with tcp reset
	ip6 daddr @wdtt_doh6 udp dport { 443, 853 } drop
	# IPv6 leak guard: drop v6 to bypass domains -> clients fall back to v4.
	ip6 daddr @wdtt_dst6 drop
}
EOF

	fw4 reload >/dev/null 2>&1 || /etc/init.d/firewall reload >/dev/null 2>&1 || true

	# Assemble the source lists, then resolve them into the nft sets (no dnsmasq).
	if [ -x /usr/sbin/wdtt-genlists ]; then
		/usr/sbin/wdtt-genlists || warn "генерация списков вернула ошибку"
	else
		warn "wdtt-genlists не найден — списки соберутся при первом «Обновить списки» в UI"
	fi
	n=$(nft list set inet fw4 "$NFSET" 2>/dev/null | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | wc -l)
	echo "-> сеты готовы, IP в wdtt_dst4: $n"
}

setup_firewall() {
	say "[6/7] Файрвол: зона wdtt (masq + MSS clamp), форвардинг lan→wdtt..."
	uci -q delete firewall.wdtt_zone 2>/dev/null || true
	uci set firewall.wdtt_zone=zone
	uci set firewall.wdtt_zone.name='wdtt'
	uci set firewall.wdtt_zone.input='REJECT'
	uci set firewall.wdtt_zone.output='ACCEPT'
	uci set firewall.wdtt_zone.forward='REJECT'
	uci set firewall.wdtt_zone.masq='1'
	uci set firewall.wdtt_zone.mtu_fix='1'
	uci -q delete firewall.wdtt_zone.device 2>/dev/null || true
	uci add_list firewall.wdtt_zone.device='wgturn'
	uci -q delete firewall.wdtt_fwd 2>/dev/null || true
	uci set firewall.wdtt_fwd=forwarding
	uci set firewall.wdtt_fwd.src='lan'
	uci set firewall.wdtt_fwd.dest='wdtt'
	uci commit firewall
	/etc/init.d/firewall reload >/dev/null 2>&1 || true
	echo "-> зона применена"
}

install_luci() {
	say "[+] Веб-интерфейс (luci-app-wdtt)..."
	local src="$OPENWRT/luci-app-wdtt"
	if [ ! -d "$src" ]; then
		warn "каталог luci-app-wdtt не найден — пропускаю UI"
		return 0
	fi
	cp -a "$src/usr" / 2>/dev/null
	cp -a "$src/www" / 2>/dev/null
	cp -a "$src/etc" / 2>/dev/null
	chmod +x /usr/libexec/rpcd/wdtt /usr/sbin/wdtt-genlists /usr/sbin/wdtt-resolve 2>/dev/null
	rm -f /tmp/luci-indexcache /tmp/luci-modulecache/* 2>/dev/null
	/etc/init.d/rpcd reload 2>/dev/null
	echo "-> интерфейс: LuCI → Службы → WDTT"
}

enable_start() {
	say "[7/7] Запуск службы..."
	[ "$(uci -q get wdtt.settings.enabled)" = "1" ] || { uci set wdtt.settings.enabled='1'; uci commit wdtt; }
	/etc/init.d/wdtt-client enable
	/etc/init.d/wdtt-client restart
	sleep 2
	echo "wgturn:"; ip -brief addr show wgturn 2>/dev/null || echo "(ещё поднимается — см. logread)"
	echo "nft set $NFSET (кол-во элементов):"; nft list set inet fw4 $NFSET 2>/dev/null | grep -c '\.' || true
	cat <<EOF

Готово (режим selective). Диагностика:
  logread -e wdtt -f
  ip addr show wgturn            # 10.66.66.x = туннель поднят
  nft list set inet fw4 $NFSET | head
  ip rule ; ip route show table 100
EOF
}

main() {
	[ "$(id -u)" = 0 ] || { err "нужен root"; exit 1; }
	parse_args "$@"
	derive_secrets
	echo "========================================================================="
	echo " Установка WDTT-клиента (WireGuard-over-TURN, selective routing)"
	[ -n "$WDTT_SLOT" ] && echo " slot=$WDTT_SLOT (непересекающиеся звонки)"
	echo "========================================================================="
	ensure_deps
	check_resolver
	install_files
	install_config
	install_luci
	setup_nft_and_domains
	setup_firewall
	enable_start
}

main "$@"
