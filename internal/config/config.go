// Package config holds the runtime configuration for the headless WDTT client.
package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the full runtime configuration. Values are populated from
// command-line flags; each flag also falls back to an environment variable
// (WDTT_*) so the procd init script can pass them either way.
type Config struct {
	Peer       string        // server DTLS endpoint, host:port (e.g. your-server:56000)
	Password   string        // owner password
	DeviceID   string        // stable unique per-router id
	Listen     string        // local UDP the kernel WG peer points at (127.0.0.1:9000)
	Workers    int           // number of TURN workers (multiple of 9)
	MTU        int           // WG MTU (0 => core default 1300)
	TurnHost   string        // optional TURN host override
	TurnPort   string        // optional TURN port override
	HashesURL  string        // http(s) URL returning VK call links, one per line
	HashesFile string        // local file with VK call links (alternative to URL)
	MaxHashes  int           // cap on how many call tokens to hand the engine (0 = all)
	Refresh    time.Duration // how often to refetch hashes and recycle the session

	Mode     string // routing mode: selective | lan-all | full
	WgIface  string // kernel wireguard interface name (wgturn)
	LanIface string // LAN bridge whose forwarded traffic is tunneled (lan-all mode)
	Table    int    // policy routing table id
	RulePref int    // ip rule preference (lan-all mode)

	// Failover: stay dormant while NetShift's uplink is healthy, activate (and
	// take routing priority) only when it goes down.
	Failover         bool
	NetshiftIface    string        // NetShift uplink wg interface to watch (e.g. AWG)
	NetshiftStale    time.Duration // handshake age after which NetShift is "down"
	FailoverInterval time.Duration // how often to poll NetShift health
	FailoverPref     int           // ip rule pref when failed over (below NetShift's)
}

// Parse builds a Config from flags with WDTT_* env fallbacks.
func Parse(args []string) (*Config, error) {
	fs := flag.NewFlagSet("wdtt-client", flag.ContinueOnError)
	c := &Config{}

	fs.StringVar(&c.Peer, "peer", env("WDTT_PEER", ""), "server DTLS endpoint host:port")
	fs.StringVar(&c.Password, "password", env("WDTT_PASSWORD", ""), "owner password")
	fs.StringVar(&c.DeviceID, "device-id", env("WDTT_DEVICE_ID", ""), "stable unique device id")
	fs.StringVar(&c.Listen, "listen", env("WDTT_LISTEN", "127.0.0.1:9000"), "local UDP listen for the WG peer")
	fs.IntVar(&c.Workers, "workers", envInt("WDTT_WORKERS", 9), "number of TURN workers (multiple of 9)")
	fs.IntVar(&c.MTU, "mtu", envInt("WDTT_MTU", 1280), "WireGuard MTU")
	fs.StringVar(&c.TurnHost, "turn-host", env("WDTT_TURN_HOST", ""), "optional TURN host override")
	fs.StringVar(&c.TurnPort, "turn-port", env("WDTT_TURN_PORT", ""), "optional TURN port override")
	fs.StringVar(&c.HashesURL, "hashes-url", env("WDTT_HASHES_URL", ""), "URL returning VK call links (one per line)")
	fs.StringVar(&c.HashesFile, "hashes-file", env("WDTT_HASHES_FILE", ""), "local file with VK call links")
	fs.IntVar(&c.MaxHashes, "max-hashes", envInt("WDTT_MAX_HASHES", 4), "max call tokens to use (0 = all)")
	fs.DurationVar(&c.Refresh, "refresh", envDur("WDTT_REFRESH", 15*time.Minute), "hash refresh / session recycle interval")

	fs.StringVar(&c.Mode, "mode", env("WDTT_MODE", "selective"), "routing mode: selective | lan-all | full")
	fs.StringVar(&c.WgIface, "wg-iface", env("WDTT_WG_IFACE", "wgturn"), "kernel wireguard interface name")
	fs.StringVar(&c.LanIface, "lan-iface", env("WDTT_LAN_IFACE", "br-lan"), "LAN interface (lan-all mode)")
	fs.IntVar(&c.Table, "table", envInt("WDTT_TABLE", 100), "policy routing table id")
	fs.IntVar(&c.RulePref, "rule-pref", envInt("WDTT_RULE_PREF", 30000), "ip rule preference (lan-all mode)")

	fs.BoolVar(&c.Failover, "failover", envBool("WDTT_FAILOVER", false), "activate only when NetShift uplink is down")
	fs.StringVar(&c.NetshiftIface, "netshift-iface", env("WDTT_NETSHIFT_IFACE", "AWG"), "NetShift uplink wg interface to watch")
	fs.DurationVar(&c.NetshiftStale, "netshift-stale", envDur("WDTT_NETSHIFT_STALE", 180*time.Second), "handshake age => NetShift down")
	fs.DurationVar(&c.FailoverInterval, "failover-interval", envDur("WDTT_FAILOVER_INTERVAL", 20*time.Second), "NetShift health poll interval")
	fs.IntVar(&c.FailoverPref, "failover-pref", envInt("WDTT_FAILOVER_PREF", 100), "ip rule pref when failed over (below NetShift's)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.Peer == "" {
		return fmt.Errorf("peer is required (WDTT_PEER / -peer)")
	}
	if c.Password == "" {
		return fmt.Errorf("password is required (WDTT_PASSWORD / -password)")
	}
	if c.HashesURL == "" && c.HashesFile == "" {
		return fmt.Errorf("one of hashes-url or hashes-file is required")
	}
	if c.DeviceID == "" {
		return fmt.Errorf("device-id is required (WDTT_DEVICE_ID / -device-id)")
	}
	switch c.Mode {
	case "", "selective":
		c.Mode = "selective"
	case "lan-all", "full":
	default:
		return fmt.Errorf("invalid mode %q (want selective|lan-all|full)", c.Mode)
	}
	return nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}
