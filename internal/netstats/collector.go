package netstats

import (
	"context"
	"fmt"
	"os"
	"time"
)

const procNetDevPath = "/proc/net/dev"

// Collector reads interface counters from a Linux procfs file.
type Collector struct {
	path string
	now  func() time.Time
}

// NewCollector creates a collector backed by the host's /proc/net/dev.
func NewCollector() *Collector {
	return &Collector{path: procNetDevPath, now: time.Now}
}

// NewCollectorFromPath creates a collector for path. It is useful for tests and
// for callers running with a procfs mounted at a non-default location.
func NewCollectorFromPath(path string) *Collector {
	return &Collector{path: path, now: time.Now}
}

// Collect reads one timestamped counter snapshot.
func (c *Collector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	file, err := os.Open(c.path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open %s: %w", c.path, err)
	}
	defer file.Close()

	interfaces, err := Parse(file)
	if err != nil {
		return Snapshot{}, fmt.Errorf("parse %s: %w", c.path, err)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	return Snapshot{At: now(), Interfaces: interfaces}, nil
}
