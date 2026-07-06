//go:build linux

// Package netcfg applies the server-issued WireGuard config to the OpenWrt
// kernel and wires up routing. Three modes:
//
//   - "selective" (default, NetShift-style): only destinations whose IPs were
//     placed in the nftables set by dnsmasq (from the allow-domains lists) are
//     marked and policy-routed into the tunnel. The nft set, the marking rule
//     and the `ip rule fwmark ... lookup <table>` are installed once by
//     install.sh; here we only own the tunnel interface and the table's
//     default route. Everything else exits the real WAN directly.
//
//   - "lan-all": every packet forwarded from the LAN goes through the tunnel
//     (via `ip rule iif <lan>`), the router itself stays on WAN.
//
//   - "full": the whole router is routed through the tunnel via the 0.0.0.0/1
//     split; the server and discovered TURN relays are pinned to the WAN gw.
//
// In every mode the router's own outbound (the Go client's packets to the VK
// TURN relays) uses the main table and never loops back into the tunnel.
package netcfg

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Mode selects the routing strategy.
type Mode string

const (
	ModeSelective Mode = "selective"
	ModeLanAll    Mode = "lan-all"
	ModeFull      Mode = "full"
)

// Options configures the applier.
type Options struct {
	Mode     Mode
	WgIface  string
	LanIface string
	Table    int
	RulePref int
	MTU      int
	// PinDirect are IPs always routed via the real gateway (full mode: the
	// server IP; TURN relays are added at runtime via AddDirectRoutes).
	PinDirect []string
}

// selectiveMark is the fwmark the nft rule (installed by install.sh) ORs onto
// packets destined for the bypass set; we route that mark into the table here.
// Masked match (0x1/0x1) so a packet that also carries other subsystems'
// marks (zapret 0x40000000, NetShift 0x100000) still matches on our bit.
const selectiveMark = "0x1/0x1"

// Applier owns the kernel state it creates and can tear it down cleanly.
type Applier struct {
	opt     Options
	mu      sync.Mutex
	gw      string
	direct  map[string]struct{}
	ruleDel []string // args for `ip rule del ...`, nil if no rule installed
	up      bool
}

// New returns an Applier for the given options.
func New(opt Options) *Applier {
	if opt.Mode == "" {
		opt.Mode = ModeSelective
	}
	return &Applier{opt: opt, direct: make(map[string]struct{})}
}

var wgQuickOnly = map[string]bool{
	"address": true, "dns": true, "mtu": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}

func parseWGConfig(conf string) (addr, mtu string, allowedIPs []string, setconf string) {
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(conf))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if parts := strings.SplitN(trimmed, "=", 2); len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			switch key {
			case "address":
				addr = val
				continue
			case "mtu":
				mtu = val
				continue
			case "allowedips":
				for _, c := range strings.Split(val, ",") {
					if c = strings.TrimSpace(c); c != "" {
						allowedIPs = append(allowedIPs, c)
					}
				}
			default:
				if wgQuickOnly[key] {
					continue
				}
			}
		}
		out.WriteString(line + "\n")
	}
	return addr, mtu, allowedIPs, out.String()
}

// Apply brings the tunnel up from a freshly issued wg config. Idempotent.
func (a *Applier) Apply(wgConf string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.teardownLocked()

	addr, confMTU, allowedIPs, setconf := parseWGConfig(wgConf)
	if addr == "" {
		return fmt.Errorf("no Address in wg config")
	}

	tmp, err := os.CreateTemp("", "wgturn-*.conf")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(setconf); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	_ = os.Chmod(tmpName, 0o600)

	iface := a.opt.WgIface
	if err := run("ip", "link", "add", "dev", iface, "type", "wireguard"); err != nil {
		return fmt.Errorf("ip link add %s: %w", iface, err)
	}
	if err := run("wg", "setconf", iface, tmpName); err != nil {
		return fmt.Errorf("wg setconf: %w", err)
	}
	_ = run("ip", "address", "flush", "dev", iface)
	if err := run("ip", "address", "add", addr, "dev", iface); err != nil {
		return fmt.Errorf("ip address add %s: %w", addr, err)
	}
	mtu := confMTU
	if a.opt.MTU > 0 {
		mtu = fmt.Sprintf("%d", a.opt.MTU)
	}
	if mtu != "" {
		_ = run("ip", "link", "set", "dev", iface, "mtu", mtu)
	}
	if err := run("ip", "link", "set", "dev", iface, "up"); err != nil {
		return fmt.Errorf("ip link set up: %w", err)
	}

	a.gw = defaultGateway()
	table := fmt.Sprintf("%d", a.opt.Table)

	switch a.opt.Mode {
	case ModeFull:
		if a.gw != "" {
			for _, ip := range a.opt.PinDirect {
				cidr := strings.TrimSpace(ip) + "/32"
				if run("ip", "route", "replace", cidr, "via", a.gw) == nil {
					a.direct[cidr] = struct{}{}
				}
			}
		}
		for _, cidr := range splitOrDefault(allowedIPs) {
			if err := run("ip", "route", "replace", cidr, "dev", iface); err != nil {
				log.Printf("[netcfg] route %s dev %s: %v", cidr, iface, err)
			}
		}
	case ModeLanAll:
		if err := run("ip", "route", "replace", "default", "dev", iface, "table", table); err != nil {
			return fmt.Errorf("ip route default table %s: %w", table, err)
		}
		pref := fmt.Sprintf("%d", a.opt.RulePref)
		del := []string{"iif", a.opt.LanIface, "lookup", table, "pref", pref}
		_ = run(ipRuleDel(del)...)
		if err := run(ipRuleAdd(del)...); err != nil {
			return fmt.Errorf("ip rule add: %w", err)
		}
		a.ruleDel = del
	default: // ModeSelective
		// nft marking of bypass-set destinations is installed by install.sh;
		// here we publish the tunnel default into the table and route the
		// marked traffic to it.
		if err := run("ip", "route", "replace", "default", "dev", iface, "table", table); err != nil {
			return fmt.Errorf("ip route default table %s: %w", table, err)
		}
		pref := fmt.Sprintf("%d", a.opt.RulePref)
		del := []string{"fwmark", selectiveMark, "lookup", table, "pref", pref}
		_ = run(ipRuleDel(del)...)
		if err := run(ipRuleAdd(del)...); err != nil {
			return fmt.Errorf("ip rule add: %w", err)
		}
		a.ruleDel = del
	}

	a.up = true
	log.Printf("[netcfg] tunnel up on %s addr=%s mtu=%s mode=%s", iface, addr, mtu, a.opt.Mode)
	return nil
}

// AddDirectRoutes pins IPs to the real gateway (full mode: TURN relays).
func (a *Applier) AddDirectRoutes(ips []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gw == "" || !a.up || a.opt.Mode != ModeFull {
		return
	}
	for _, ip := range ips {
		if ip = strings.TrimSpace(ip); ip == "" {
			continue
		}
		cidr := ip + "/32"
		if _, ok := a.direct[cidr]; ok {
			continue
		}
		if run("ip", "route", "replace", cidr, "via", a.gw) == nil {
			a.direct[cidr] = struct{}{}
		}
	}
}

// Teardown removes everything Apply created.
func (a *Applier) Teardown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.teardownLocked()
}

func (a *Applier) teardownLocked() {
	iface := a.opt.WgIface
	table := fmt.Sprintf("%d", a.opt.Table)

	if a.ruleDel != nil {
		_ = run(ipRuleDel(a.ruleDel)...)
		a.ruleDel = nil
	}
	_ = run("ip", "route", "flush", "table", table)
	for cidr := range a.direct {
		_ = run("ip", "route", "del", cidr)
		delete(a.direct, cidr)
	}
	_ = run("ip", "link", "del", "dev", iface)
	a.up = false
}

func splitOrDefault(allowedIPs []string) []string {
	for _, c := range allowedIPs {
		if c == "0.0.0.0/1" || c == "128.0.0.0/1" {
			return []string{"0.0.0.0/1", "128.0.0.0/1"}
		}
	}
	if len(allowedIPs) == 0 {
		return []string{"0.0.0.0/1", "128.0.0.0/1"}
	}
	return allowedIPs
}

func defaultGateway() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func ipRuleAdd(spec []string) []string { return append([]string{"ip", "rule", "add"}, spec...) }
func ipRuleDel(spec []string) []string { return append([]string{"ip", "rule", "del"}, spec...) }

func run(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("run: empty command")
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w — %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
