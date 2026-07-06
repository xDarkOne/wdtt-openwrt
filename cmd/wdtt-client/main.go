// Command wdtt-client is a headless WireGuard-over-TURN tunnel client for
// OpenWrt. It wraps the upstream PWDTT "core" engine (which relays a WireGuard
// session through VK TURN relays so the carrier sees only whitelisted VK call
// traffic) and applies the server-issued WireGuard config to the kernel.
//
// The process is a supervisor: it fetches fresh VK call tokens, runs one core
// session, applies the tunnel when the server hands back a WG config, and
// recycles the session when tokens rotate, the link drops, or it is signalled.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"wdtt-openwrt/internal/config"
	"wdtt-openwrt/internal/hashes"
	"wdtt-openwrt/internal/netcfg"

	"wg-turn-client/core"
)

func main() {
	log.SetFlags(0) // procd/syslog adds its own timestamps
	log.SetPrefix("")

	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pin := []string(nil)
	if cfg.Mode == "full" {
		if h := hostOf(cfg.Peer); h != "" {
			pin = []string{h}
		}
	}
	// When failing over we must out-rank NetShift's ip rule, so use the lower
	// (higher-priority) failover pref instead of the passive one.
	rulePref := cfg.RulePref
	if cfg.Failover {
		rulePref = cfg.FailoverPref
	}
	applier := netcfg.New(netcfg.Options{
		Mode:      netcfg.Mode(cfg.Mode),
		WgIface:   cfg.WgIface,
		LanIface:  cfg.LanIface,
		Table:     cfg.Table,
		RulePref:  rulePref,
		MTU:       cfg.MTU,
		PinDirect: pin,
	})
	// Always leave the box in a clean state on exit.
	defer applier.Teardown()

	log.Printf("[wdtt] starting: peer=%s device=%s listen=%s workers=%d mode=%s failover=%v",
		cfg.Peer, cfg.DeviceID, cfg.Listen, cfg.Workers, cfg.Mode, cfg.Failover)

	supervise(ctx, cfg, applier)
	log.Printf("[wdtt] stopped")
}

func supervise(ctx context.Context, cfg *config.Config, applier *netcfg.Applier) {
	src := hashes.Source{URL: cfg.HashesURL, File: cfg.HashesFile}
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second

	for ctx.Err() == nil {
		// Lazy failover: stay dormant (tunnel down) until NetShift's uplink
		// goes stale, then activate.
		if cfg.Failover {
			if !waitNetshiftDown(ctx, cfg, applier) {
				return
			}
		}

		tokens, err := hashes.Fetch(ctx, src)
		if err != nil {
			log.Printf("[wdtt] fetch hashes: %v (retry in %s)", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = 2 * time.Second
		tokens = capTokens(tokens, cfg.MaxHashes)
		log.Printf("[wdtt] got %d call tokens, connecting", len(tokens))

		reason := runSession(ctx, cfg, applier, src, tokens)
		applier.Teardown()
		if ctx.Err() != nil {
			return
		}
		switch reason {
		case reasonFatalAuth:
			log.Printf("[wdtt] server rejected auth — check password/device-id; retry in 60s")
			sleepCtx(ctx, 60*time.Second)
		case reasonRecycle:
			log.Printf("[wdtt] recycling session")
			sleepCtx(ctx, 1*time.Second)
		default:
			sleepCtx(ctx, 3*time.Second)
		}
	}
}

type reason int

const (
	reasonEnded reason = iota
	reasonError
	reasonFatalAuth
	reasonRecycle
)

func runSession(ctx context.Context, cfg *config.Config, applier *netcfg.Applier, src hashes.Source, tokens []string) reason {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c := core.New(core.Config{
		PeerAddr:    cfg.Peer,
		Password:    cfg.Password,
		Hashes:      tokens,
		Listen:      cfg.Listen,
		TurnHost:    cfg.TurnHost,
		TurnPort:    cfg.TurnPort,
		DeviceID:    cfg.DeviceID,
		Workers:     cfg.Workers,
		MTU:         cfg.MTU,
		CaptchaMode: "auto",
	})
	events, err := c.Start()
	if err != nil {
		log.Printf("[wdtt] core start: %v", err)
		return reasonError
	}

	// Stop the core when the session context is cancelled (signal / recycle).
	go func() {
		<-sctx.Done()
		c.Stop()
	}()

	// Watch for token rotation and recycle the session when the set changes.
	recycle := make(chan struct{}, 1)
	go watchHashes(sctx, src, tokens, cfg.MaxHashes, cfg.Refresh, recycle)
	go func() {
		select {
		case <-recycle:
			cancel()
		case <-sctx.Done():
		}
	}()

	// In full-tunnel mode keep pinning freshly discovered TURN relay IPs to WAN.
	if cfg.Mode == "full" {
		go pinRelays(sctx, c, applier)
	}

	// Failover: end the session as soon as NetShift's uplink recovers.
	if cfg.Failover {
		go watchNetshiftRecover(sctx, cfg, cancel)
	}

	res := reasonEnded
	for ev := range events {
		switch ev.Type {
		case core.EventEvent:
			if ev.Name == "wg_config" {
				if cfg.Mode == "full" {
					applier.AddDirectRoutes(c.GetTurnIPs())
				}
				if err := applier.Apply(ev.Data); err != nil {
					log.Printf("[wdtt] apply wg config: %v", err)
				} else {
					log.Printf("[wdtt] tunnel active ✓")
				}
			}
		case core.EventLog:
			if strings.Contains(ev.Message, "FATAL_AUTH") {
				res = reasonFatalAuth
			}
			log.Printf("[core:%s] %s", strings.ToLower(ev.Level), ev.Message)
		case core.EventError:
			log.Printf("[core:error] %s", ev.Message)
		case core.EventState:
			log.Printf("[core:state] %s", ev.Status)
		case core.EventStats:
			// stats are frequent; keep them out of the log by default
		}
	}

	// events closed => core finished. If we cancelled due to recycle and it
	// wasn't an auth failure, report a recycle so the supervisor loops quietly.
	if res == reasonEnded && sctx.Err() != nil && ctx.Err() == nil {
		return reasonRecycle
	}
	return res
}

// watchHashes periodically refetches tokens and signals recycle when they change.
func watchHashes(ctx context.Context, src hashes.Source, current []string, maxHashes int, every time.Duration, out chan<- struct{}) {
	if every <= 0 {
		every = 15 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			next, err := hashes.Fetch(ctx, src)
			if err != nil {
				log.Printf("[wdtt] refresh hashes: %v", err)
				continue
			}
			next = capTokens(next, maxHashes)
			if !hashes.Equal(next, current) {
				log.Printf("[wdtt] call tokens rotated (%d→%d), recycling", len(current), len(next))
				select {
				case out <- struct{}{}:
				default:
				}
				return
			}
		}
	}
}

// pinRelays periodically pins the core's known TURN relay IPs to the WAN
// gateway (full-tunnel only).
func pinRelays(ctx context.Context, c *core.Core, applier *netcfg.Applier) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			applier.AddDirectRoutes(c.GetTurnIPs())
		}
	}
}

// capTokens deterministically limits the token set to n (0 = no cap). Input is
// already sorted by hashes.Fetch, so the selection is stable across refetches.
func capTokens(tokens []string, n int) []string {
	if n <= 0 || len(tokens) <= n {
		return tokens
	}
	return tokens[:n]
}

// waitNetshiftDown blocks (keeping the tunnel torn down) until NetShift's
// uplink is stale. Returns false if the context is cancelled first.
func waitNetshiftDown(ctx context.Context, cfg *config.Config, applier *netcfg.Applier) bool {
	logged := false
	for {
		if !netshiftHealthy(cfg) {
			log.Printf("[wdtt] NetShift uplink %s is down — activating tunnel", cfg.NetshiftIface)
			return true
		}
		if !logged {
			log.Printf("[wdtt] NetShift uplink %s healthy — staying dormant", cfg.NetshiftIface)
			logged = true
		}
		applier.Teardown() // ensure nothing of ours is routing while dormant
		if !sleepCtx(ctx, cfg.FailoverInterval) {
			return false
		}
	}
}

// watchNetshiftRecover cancels the running session once NetShift recovers.
func watchNetshiftRecover(ctx context.Context, cfg *config.Config, cancel context.CancelFunc) {
	t := time.NewTicker(cfg.FailoverInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if netshiftHealthy(cfg) {
				log.Printf("[wdtt] NetShift uplink %s recovered — deactivating tunnel", cfg.NetshiftIface)
				cancel()
				return
			}
		}
	}
}

// netshiftHealthy reports whether NetShift's uplink interface has a recent
// handshake. A missing interface or no handshake counts as down. NetShift's
// uplink is usually AmneziaWG, which the plain `wg` tool cannot read — so we
// try `awg` first and fall back to `wg`.
func netshiftHealthy(cfg *config.Config) bool {
	out := latestHandshakes(cfg.NetshiftIface)
	if out == nil {
		return false
	}
	var newest int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if ts, err := strconv.ParseInt(f[len(f)-1], 10, 64); err == nil && ts > newest {
			newest = ts
		}
	}
	if newest == 0 {
		return false
	}
	age := time.Since(time.Unix(newest, 0))
	return age < cfg.NetshiftStale
}

// latestHandshakes returns `<tool> show <iface> latest-handshakes` output,
// trying AmneziaWG's awg tool first, then plain wg.
func latestHandshakes(iface string) []byte {
	for _, tool := range []string{"awg", "wg"} {
		if out, err := exec.Command(tool, "show", iface, "latest-handshakes").Output(); err == nil && len(out) > 0 {
			return out
		}
	}
	return nil
}

func hostOf(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
