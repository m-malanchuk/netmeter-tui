package app

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

type processCollectorStub struct {
	calls atomic.Int32
}

func (collector *processCollectorStub) Collect(context.Context) (procstats.Snapshot, error) {
	collector.calls.Add(1)
	return procstats.Snapshot{At: time.Now()}, nil
}

func (*processCollectorStub) Close() error { return nil }

func TestProcessSampleLoopCollectsOnlyWhileEnabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	collector := &processCollectorStub{}
	intervalChanges := make(chan time.Duration, 1)
	modeChanges := make(chan bool, 1)
	refreshes := make(chan struct{}, 1)
	results := make(chan sampleResult, 1)
	done := make(chan struct{})
	go func() {
		processSampleLoop(ctx, collector, time.Hour, intervalChanges, modeChanges, refreshes, results)
		close(done)
	}()

	modeChanges <- true
	waitForProcessResult(t, results)
	if got := collector.calls.Load(); got != 1 {
		t.Fatalf("collections after enabling = %d, want 1", got)
	}

	modeChanges <- false
	waitForChannelDrain(t, modeChanges)
	publishProcessRefresh(refreshes)
	select {
	case <-results:
		t.Fatal("disabled process worker collected after refresh")
	case <-time.After(20 * time.Millisecond):
	}

	modeChanges <- true
	waitForProcessResult(t, results)
	if got := collector.calls.Load(); got != 2 {
		t.Fatalf("collections after re-enabling = %d, want 2", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("process worker did not stop after cancellation")
	}
}

func waitForChannelDrain[T any](t *testing.T, channel chan T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(channel) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for worker command")
		}
		runtime.Gosched()
	}
}

func waitForProcessResult(t *testing.T, results <-chan sampleResult) {
	t.Helper()
	select {
	case result := <-results:
		if result.processErr != nil {
			t.Fatalf("process collection failed: %v", result.processErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process collection")
	}
}
