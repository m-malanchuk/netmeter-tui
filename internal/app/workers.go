package app

import (
	"context"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

type sampleResult struct {
	snapshot        netstats.Snapshot
	err             error
	processSnapshot procstats.Snapshot
	processErr      error
}

func interfaceSampleLoop(ctx context.Context, collector InterfaceCollector, interval time.Duration, intervalChanges <-chan time.Duration, results chan<- sampleResult) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(results)

	for {
		select {
		case <-ctx.Done():
			return
		case nextInterval := <-intervalChanges:
			if nextInterval <= 0 || nextInterval == interval {
				continue
			}
			ticker.Reset(nextInterval)
			interval = nextInterval
		case <-ticker.C:
			snapshot, err := collector.Collect(ctx)
			select {
			case results <- sampleResult{snapshot: snapshot, err: err}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Process inspection can be noticeably slower on hosts with many processes.
// A bounded collection keeps shutdown responsive without blocking interface I/O.
const processCollectionTimeout = 10 * time.Second

func processSampleLoop(ctx context.Context, collector ProcessCollector, interval time.Duration, intervalChanges <-chan time.Duration, modeChanges <-chan bool, refreshes <-chan struct{}, results chan<- sampleResult) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(results)
	enabled := false

	collect := func() bool {
		collectionCtx, cancel := context.WithTimeout(ctx, processCollectionTimeout)
		snapshot, err := collector.Collect(collectionCtx)
		cancel()
		select {
		case results <- sampleResult{processSnapshot: snapshot, processErr: err}:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case nextInterval := <-intervalChanges:
			if nextInterval <= 0 || nextInterval == interval {
				continue
			}
			ticker.Reset(nextInterval)
			interval = nextInterval
		case nextEnabled := <-modeChanges:
			wasEnabled := enabled
			enabled = nextEnabled
			if enabled && !wasEnabled && !collect() {
				return
			}
		case <-refreshes:
			if enabled && !collect() {
				return
			}
		case <-ticker.C:
			if enabled && !collect() {
				return
			}
		}
	}
}

func publishProcessRefresh(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func publishLatest[T any](ch chan T, value T) {
	select {
	case ch <- value:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- value:
	default:
	}
}
