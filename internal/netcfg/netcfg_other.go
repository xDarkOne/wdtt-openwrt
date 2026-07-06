//go:build !linux

// Stub so the package builds on non-Linux dev machines. The real client only
// ever runs on OpenWrt (Linux); these no-ops let editors and `go build` work.
package netcfg

import "fmt"

// Mode selects the routing strategy.
type Mode string

const (
	ModeSelective Mode = "selective"
	ModeLanAll    Mode = "lan-all"
	ModeFull      Mode = "full"
)

// Options mirrors the linux build.
type Options struct {
	Mode      Mode
	WgIface   string
	LanIface  string
	Table     int
	RulePref  int
	MTU       int
	PinDirect []string
}

// Applier is a no-op outside Linux.
type Applier struct{ opt Options }

// New returns a no-op applier.
func New(opt Options) *Applier { return &Applier{opt: opt} }

// Apply is unsupported off Linux.
func (a *Applier) Apply(string) error { return fmt.Errorf("netcfg: only supported on linux") }

// AddDirectRoutes is a no-op off Linux.
func (a *Applier) AddDirectRoutes([]string) {}

// Teardown is a no-op off Linux.
func (a *Applier) Teardown() {}
