// Package netstats reads Linux network interface counters.
package netstats

import "time"

// Counters contains the byte counters reported for one network interface.
type Counters struct {
	Name    string
	RXBytes uint64
	TXBytes uint64
}

// Snapshot is a timestamped collection of interface counters.
type Snapshot struct {
	At         time.Time
	Interfaces []Counters
}
