# wdtt-openwrt

Headless **WDTT** (WireGuard-over-TURN Tunnel) client for **OpenWrt**, with
**NetShift-style selective routing**: only blocked resources (Telegram,
Instagram/Meta, …) go through the tunnel, everything else exits the real WAN
directly.

The tunnel's carrier traffic is disguised as a VK voice call (relayed through
VK's own TURN servers), which mobile carriers keep on their "white list" — so
the throttling/DPI sees only VK call media, and the router bypasses the block
for the whole LAN.

Derivative of PWDTT / proxy-turn-vk (**GPL-3.0**, see `LICENSE`). The upstream
engine is vendored untouched in `third_party/wg-turn-client/core`; everything
else is the OpenWrt glue.

---

## How it works

```
LAN client ─▶ dnsmasq resolves a blocked domain ─▶ IP added to nft set wdtt_dst4
LAN client ─▶ packet to that IP ─▶ nft marks it (0x1) ─▶ ip rule ─▶ table 100
                                                                       │ default
                                                                       ▼
                          wgturn (kernel WireGuard, endpoint 127.0.0.1:9000)
                                                                       ▼
                          wdtt-client (Go daemon)  ── RTP/AEAD over DTLS ──▶
                          VK TURN relay (looks like a VK call, whitelisted)
                                                                       ▼
                          wdtt-server (VPS) ─▶ internet
```

Traffic **not** matched by the domain set is never marked, so it uses the main
routing table and goes straight out the WAN. The router's own outbound — the Go
daemon's packets to the VK TURN relays and the server — is also unmarked and
never loops back into the tunnel.

### The pieces

* **`wdtt-client`** (on the router) — joins a live VK call anonymously to get a
  TURN relay allocation, shuttles WireGuard packets through it to the server,
  and applies the server-issued per-device WG config (`10.66.66.x`) to the
  kernel `wgturn` interface. It also owns the `ip rule fwmark → table` and the
  table's default route.
* **nft set `wdtt_dst4` + dnsmasq** — dnsmasq (built with nftset support) adds
  the resolved IPs of the blocked domains to the set; an `/etc/nftables.d`
  rule marks packets headed there. Domain lists come from
  [itdoginfo/allow-domains](https://github.com/itdoginfo/allow-domains) (the
  same community lists NetShift/podkop use), plus the router's own zapret
  `zapret-hosts-user.txt`.
* **`wdtt-linkd`** (on the VPS) — a tiny token-guarded endpoint that returns 4
  fresh VK call links on demand, read live from the whitelist-bypass creator's
  rotating file. The router refetches them periodically. With `?slot=N` it hands
  each router a distinct, non-overlapping slice of the pool (see *Multiple
  routers* below); without it, 4 random links.

### Routing modes (`option mode`)

| mode        | what goes through the tunnel                              |
|-------------|-----------------------------------------------------------|
| `selective` | only IPs of blocked domains (default, NetShift-style)     |
| `lan-all`   | all LAN client traffic (`ip rule iif br-lan`)            |
| `full`      | the whole router (0.0.0.0/1 split; server+relays pinned) |

### Coexistence with NetShift / zapret

WDTT marks packets with `meta mark set mark or 0x1` (a spare bit) and matches
its `ip rule` on `0x1/0x1`, so it never clobbers **zapret** (`0x40000000`) or
**NetShift** (`0x100000`). zapret is orthogonal anyway — tunneled traffic is
encrypted, so its DPI layer simply doesn't see it. For a domain routed by both
NetShift and WDTT, NetShift's lower-pref rule wins, so WDTT can't break it.

### Auto-failover with NetShift (`option failover 1`)

Lazy failover for dacha SIM routers: WDTT stays **dormant** while NetShift's
kernel WireGuard uplink (`AWG`) has a fresh handshake. When that uplink goes
stale (e.g. a whitelist shutdown blocks its endpoint), WDTT **auto-activates**
and raises its `ip rule` above NetShift's, so the same domains transparently
switch to the VK tunnel. When the uplink recovers, WDTT tears down and NetShift
reclaims. Point the same lists at both and it just works.

---

## Layout

```
UI tabs: Overview (status + traffic stats), Settings (all options),
Lists (podkop-style community picker + custom/local/remote lists),
Diagnostics (server/exit/DNS checks + live log). NetShift-inspired extras:
DoH/DoT blocking, IPv6 leak guard, auto-update, tunnel-fallback list fetch.

cmd/wdtt-client/         router daemon: fetch tokens → run core → apply WG + routing
cmd/wdtt-linkd/          VPS endpoint: 4 fresh call links on demand
internal/config/         flag + WDTT_* env configuration
internal/hashes/         fetch & normalize VK call tokens
internal/netcfg/         apply WG to the kernel + selective/lan-all/full routing
third_party/wg-turn-client/  vendored upstream PWDTT engine (GPL-3.0)
openwrt/etc/config/wdtt      UCI config template
openwrt/etc/init.d/wdtt-client  procd service
bootstrap.sh             one-line entry: download repo tarball → run installer
scripts/build.sh         cross-compile via docker golang (client + linkd)
scripts/install.sh       router installer (deps, nft, dnsmasq, firewall, slot)
deploy.env.example       template for your secrets (deploy.env is gitignored)
server/wdtt-linkd.service    systemd unit for the endpoint
server/setup-linkd.sh        install the endpoint on the VPS
```

---

## Build

Needs Docker (no Go toolchain required):

```sh
scripts/build.sh              # client arm64 (all Xiaomi/Cudy) + linkd amd64
scripts/build.sh arm64 armv7 mipsle amd64
```

Binaries land in `dist/`.

## Server (one-time, authorize on your VPS)

```sh
# copy dist/wdtt-linkd-linux-amd64 next to server/setup-linkd.sh as ./wdtt-linkd
sh server/setup-linkd.sh
# prints:  http://<vps>:56090/<token>/links?n=4
```

## Router — install in one line

On each router, replace `YOURUSER` with your GitHub user and fill in your
server / token / owner-password. **No secrets live in the repo** — they are
passed here at install time.

```sh
wget -qO- https://raw.githubusercontent.com/YOURUSER/wdtt-openwrt/main/bootstrap.sh \
  | WDTT_SERVER=your-server WDTT_TOKEN=your-token WDTT_PASSWORD='owner-pass' \
    sh -s -- --slot 0
```

`bootstrap.sh` downloads the repo and runs `scripts/install.sh`. The package
manager is auto-detected, so the **same command works on OpenWrt 24.10 (opkg)
and 25.x (apk)**.

Prefer not to pipe from the net? Clone and run locally:

```sh
git clone https://github.com/YOURUSER/wdtt-openwrt && cd wdtt-openwrt
cp deploy.env.example deploy.env      # fill in server / token / password
. ./deploy.env && sh scripts/install.sh --slot 0
```

The installer installs `wireguard-tools`/`kmod-wireguard` (and `dnsmasq-full`
if the stock dnsmasq lacks nftset), builds the nft set + marking rule, pulls the
allow-domains list + your zapret hostlist into dnsmasq, sets up the firewall
zone (masq + MSS clamp, `lan → wdtt`), generates a stable `device_id`, drops in
the LuCI app, and starts the service. Re-running it upgrades in place (existing
`/etc/config/wdtt` options are kept; only the ones you pass are updated).

> **Note (daily-driver routers):** if dnsmasq is the stock build (no `nftset`),
> the installer swaps in `dnsmasq-full`, which briefly restarts DNS. Do it
> during a quiet moment.

### Multiple routers — non-overlapping calls

The VK call pool is shared, so give each router a distinct **slot** and they
never draw the same call. Each slot owns a contiguous window of the sorted pool;
at the default 4 calls/router the pool just needs `slots × 4` live calls.

| router | install flag |
|--------|--------------|
| #1     | `--slot 0`   |
| #2     | `--slot 1`   |
| #3     | `--slot 2`   |

Omit `--slot` to fall back to a random pick (fine for a single router). The slot
is baked into `hashes_url` as `&slot=N`; change it any time in
Settings → *URL ссылок на звонки* or `uci set wdtt.settings.hashes_url=…`.

---

## Configuration (`/etc/config/wdtt`)

| option        | meaning                                                        |
|---------------|----------------------------------------------------------------|
| `enabled`     | master switch (installer sets `1`)                            |
| `peer`        | server DTLS endpoint `host:port`                             |
| `password`    | owner password (matches `wdtt-server -password`)            |
| `device_id`   | stable id; server allocates one `10.66.66.x` per id          |
| `hashes_url`  | `wdtt-linkd` endpoint returning fresh call links            |
| `max_hashes`  | call tokens to use at once (4, like the Android app)        |
| `mode`        | `selective` (default) / `lan-all` / `full`                  |
| `listen`      | local UDP for the WG peer (`127.0.0.1:9000`)                |
| `workers`     | TURN workers — **9 per call-hash**, so set `= max_hashes × 9` (36 for 4 hashes) to actually use all calls in parallel (~4-5× faster) |
| `mtu`         | WG MTU (default `1280`)                                     |
| `table` / `rule_pref` | policy-routing table / ip-rule preference           |
| `refresh`     | token refetch / session recycle interval (`15m`)           |
| `auto_update` / `auto_update_hour` | daily cron refresh of the lists          |
| `lists_via_tunnel` | fetch lists via the tunnel if GitHub is blocked (`auto`) |
| `block_doh`   | block external DoH/DoT (dibdot list) so clients use router DNS |
| `block_ipv6`  | drop IPv6 to bypass domains → clients fall back to tunneled IPv4 |

Change the bypass list via `WDTT_DOMAINS_URL` at install time (any
allow-domains `*-dnsmasq-nfset.lst`). Apply config changes with
`/etc/init.d/wdtt-client restart`.

---

## Operating

```sh
logread -e wdtt -f                       # live client log
ip addr show wgturn                      # 10.66.66.x ⇒ tunnel up
wg show wgturn                           # WireGuard peer + handshake
nft list set inet fw4 wdtt_dst4 | head   # resolved bypass IPs
ip rule ; ip route show table 100        # policy routing
```

### Troubleshooting

* **`server rejected auth`** — wrong `password` or a `device_id` already bound
  elsewhere. Pick a fresh `device_id`.
* **No `wgturn` address** — daemon never got a WG config. Check `hashes_url`
  returns links and the server is up.
* **Blocked site still throttled** — its domain isn't in the set. Add it to
  `zapret-hosts-user.txt` (or the list) and `/etc/init.d/dnsmasq restart`; the
  set fills as clients re-resolve. Check `nft list set inet fw4 wdtt_dst4`.
* **Everything routes through the tunnel** — you're in `lan-all`/`full` mode, or
  a client uses DoH/hardcoded DNS so dnsmasq never sees the lookup.

---

## Notes & caveats

* Selective routing is **IPv4-only** for now; if a client reaches a blocked
  service over IPv6 it won't be tunneled. Disable IPv6 on the LAN for those
  clients, or use `lan-all`/`full`.
* Uses the server's live VK calls as TURN relays (kept fresh by the headless
  creator). If that subsystem stalls, new sessions fail until it recovers.
* `password` in `/etc/config/wdtt` is a secret (`0600`, passed via env, not
  `ps`-visible). The `wdtt-linkd` token travels in the URL over plain HTTP —
  put it behind TLS if that matters to you.
* Relaying through VK infrastructure is a grey area w.r.t. VK's ToS, same as the
  upstream apps. Use on infrastructure you control.
