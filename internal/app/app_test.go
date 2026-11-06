package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

func TestAdjustIntervalWalksPresets(t *testing.T) {
	tests := []struct {
		name      string
		current   time.Duration
		direction int
		want      time.Duration
	}{
		{name: "increase from default", current: 500 * time.Millisecond, direction: 1, want: time.Second},
		{name: "decrease from one second", current: time.Second, direction: -1, want: 500 * time.Millisecond},
		{name: "increase arbitrary value", current: 750 * time.Millisecond, direction: 1, want: time.Second},
		{name: "decrease arbitrary value", current: 750 * time.Millisecond, direction: -1, want: 500 * time.Millisecond},
		{name: "lower bound", current: intervalPresets[0], direction: -1, want: intervalPresets[0]},
		{name: "upper bound", current: intervalPresets[len(intervalPresets)-1], direction: 1, want: intervalPresets[len(intervalPresets)-1]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adjustInterval(test.current, test.direction); got != test.want {
				t.Fatalf("adjustInterval(%s, %d) = %s, want %s", test.current, test.direction, got, test.want)
			}
		})
	}
}

func TestDefaultIntervalIsWithinSupportedRange(t *testing.T) {
	if intervalPresets[0] != minimumInterval {
		t.Fatalf("first interval preset = %s, want minimum %s", intervalPresets[0], minimumInterval)
	}
	if intervalPresets[len(intervalPresets)-1] <= intervalPresets[0] {
		t.Fatalf("interval presets are not increasing: %v", intervalPresets)
	}
}

func TestRunRejectsUnknownThemeBeforeTerminalSetup(t *testing.T) {
	err := Run(context.Background(), Config{Interval: 500 * time.Millisecond, Theme: "missing"})
	if err == nil || !strings.Contains(err.Error(), "unknown theme") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestPublishProcessRefreshCoalescesRequests(t *testing.T) {
	refreshes := make(chan struct{}, 1)
	publishProcessRefresh(refreshes)
	publishProcessRefresh(refreshes)
	if len(refreshes) != 1 {
		t.Fatalf("queued refreshes = %d, want 1", len(refreshes))
	}
}

func TestPublishLatestReplacesQueuedValue(t *testing.T) {
	values := make(chan int, 1)
	publishLatest(values, 1)
	publishLatest(values, 2)
	if got := <-values; got != 2 {
		t.Fatalf("queued value = %d, want 2", got)
	}
}

type interfaceCollectorStub struct {
	snapshot netstats.Snapshot
	calls    atomic.Int32
}

func (collector *interfaceCollectorStub) Collect(context.Context) (netstats.Snapshot, error) {
	collector.calls.Add(1)
	return collector.snapshot, nil
}

type closeTrackingProcessCollector struct {
	closed atomic.Bool
}

func (*closeTrackingProcessCollector) Collect(context.Context) (procstats.Snapshot, error) {
	return procstats.Snapshot{At: time.Unix(1, 0)}, nil
}

func (collector *closeTrackingProcessCollector) Close() error {
	collector.closed.Store(true)
	return nil
}

func TestRunReturnsCleanlyWhenContextIsAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := &cancelAwareInterfaceCollectorStub{}

	err := Run(ctx, Config{
		Interval:  500 * time.Millisecond,
		Theme:     "adwaita-dark",
		Collector: collector,
		ScreenFactory: func() (tcell.Screen, error) {
			t.Fatal("screen initialized after context cancellation")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error = %v, want nil", err)
	}
	if got := collector.calls.Load(); got != 1 {
		t.Fatalf("initial collection calls = %d, want 1", got)
	}
}

type contextIgnoringInterfaceCollectorStub struct{}

func (contextIgnoringInterfaceCollectorStub) Collect(context.Context) (netstats.Snapshot, error) {
	return netstats.Snapshot{At: time.Unix(1, 0)}, nil
}

func TestRunSkipsScreenAfterInitialCollectionWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, Config{
		Interval:  500 * time.Millisecond,
		Theme:     "adwaita-dark",
		Collector: contextIgnoringInterfaceCollectorStub{},
		ScreenFactory: func() (tcell.Screen, error) {
			t.Fatal("screen initialized after initial collection observed cancellation")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("Run returned error = %v, want nil", err)
	}
}

func TestRunCleansUpScreenWorkersAndProcessCollector(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	processCollector := &closeTrackingProcessCollector{}
	collector := &interfaceCollectorStub{snapshot: netstats.Snapshot{
		At:         time.Unix(1, 0),
		Interfaces: []netstats.Counters{{Name: "eth0"}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			Interval:         50 * time.Millisecond,
			Theme:            "adwaita-dark",
			Collector:        collector,
			ProcessCollector: processCollector,
			ScreenFactory: func() (tcell.Screen, error) {
				if err := screen.Init(); err != nil {
					return nil, err
				}
				close(ready)
				return screen, nil
			},
		})
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("screen was not initialized")
	}
	cancel()
	err := <-done
	if err != nil {
		t.Fatalf("Run returned error = %v, want nil", err)
	}
	if !processCollector.closed.Load() {
		t.Fatal("process collector was not closed")
	}
}

type cancelAwareInterfaceCollectorStub struct {
	calls atomic.Int32
}

func (collector *cancelAwareInterfaceCollectorStub) Collect(ctx context.Context) (netstats.Snapshot, error) {
	collector.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return netstats.Snapshot{}, err
	}
	return netstats.Snapshot{}, nil
}
