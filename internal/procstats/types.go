// Package procstats collects best-effort per-process network observations on Linux.
package procstats

import "time"

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
