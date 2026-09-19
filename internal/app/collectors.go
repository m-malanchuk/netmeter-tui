package app

import (
	"context"

	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

// InterfaceCollector provides timestamped per-interface counter snapshots.
// The narrow contract keeps application orchestration independent of procfs I/O.
type InterfaceCollector interface {
	Collect(context.Context) (netstats.Snapshot, error)
}

// ProcessCollector provides per-process traffic snapshots and owns any
// operating-system resources used while collecting them.
type ProcessCollector interface {
	Collect(context.Context) (procstats.Snapshot, error)
	Close() error
}
