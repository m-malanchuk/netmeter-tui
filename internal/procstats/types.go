// Package procstats collects best-effort per-process network observations on Linux.
package procstats

import (
	"fmt"
	"strings"
	"time"
)

type warningCode uint8

const (
	warningTCPFallback warningCode = iota + 1
	warningOwnerRefresh
	warningUDPSocketTable
	warningUDPFallback
	warningUDPAccounting
)

// collectorWarning keeps the cause category separate from its user-facing
// rendering. The application still receives the stable Snapshot.Warning text,
// while the collector can deduplicate and classify warnings before rendering.
type collectorWarning struct {
	code   warningCode
	detail string
}

func (warning collectorWarning) message() string {
	detail := strings.TrimSpace(warning.detail)
	switch warning.code {
	case warningTCPFallback:
		return fmt.Sprintf("TCP byte counters unavailable (%s); showing connection counts for affected families only", detail)
	case warningOwnerRefresh:
		return "process owner refresh unavailable: " + detail
	case warningUDPSocketTable:
		return "UDP socket table unavailable: " + detail
	default:
		return detail
	}
}

// ProcessCounters contains bytes observed since the previous collection and
// the number of currently visible TCP/UDP sockets. The byte fields are
// interval deltas, not cumulative kernel counters.
type ProcessCounters struct {
	PID         int
	StartTime   uint64
	Name        string
	Interface   string
	RXBytes     uint64
	TXBytes     uint64
	Connections int
}

// Snapshot is a timestamped collection of process observations.
type Snapshot struct {
	At        time.Time
	Processes []ProcessCounters
	Warning   string
}
