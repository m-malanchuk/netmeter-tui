package app

import (
	"context"
	"strings"
	"testing"
	"time"
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
