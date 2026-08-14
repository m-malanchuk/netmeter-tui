package monitor

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
)

func TestFirstSampleHasNoSpike(t *testing.T) {
	monitor := New(8)
	at := time.Unix(100, 0)
	monitor.Update(snapshot(at, counters("eth0", 100, 200)))
	stats, ok := monitor.Stats("eth0")
	if !ok {
		t.Fatal("Stats() did not find interface")
	}
	if stats.RX.Current != 0 || stats.TX.Current != 0 || stats.RX.Average != 0 || stats.TX.Maximum != 0 {
		t.Fatalf("first stats = %+v, want zero rates", stats)
	}
	if stats.RX.Total != 100 || stats.TX.Total != 200 {
		t.Fatalf("first totals = RX %d TX %d, want 100 and 200", stats.RX.Total, stats.TX.Total)
	}
}

func TestRatesUseElapsedDurationAndAggregate(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 200)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 200, 400)))
	monitor.Update(snapshot(start.Add(3*time.Second), counters("eth0", 600, 800)))

	stats, _ := monitor.Stats("eth0")
	assertClose(t, stats.RX.Current, 200)
	assertClose(t, stats.TX.Current, 200)
	assertClose(t, stats.RX.Average, 166.6666667)
	assertClose(t, stats.TX.Average, 200)
	assertClose(t, stats.RX.Maximum, 200)
	assertClose(t, stats.TX.Maximum, 200)
	if stats.RX.Total != 600 || stats.TX.Total != 800 {
		t.Fatalf("totals = RX %d TX %d, want 600 and 800", stats.RX.Total, stats.TX.Total)
	}
	if got, want := stats.RXHistory, []float64{100, 200}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("RX history = %v, want %v", got, want)
	}
}

func TestCounterResetStartsNewGeneration(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 1000, 2000)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 2000, 3000)))
	notice := monitor.Update(snapshot(start.Add(2*time.Second), counters("eth0", 10, 20)))
	if !strings.Contains(notice, "counter reset detected on eth0") {
		t.Fatalf("reset notice = %q, want counter reset notice", notice)
	}

	stats, _ := monitor.Stats("eth0")
	if stats.RX.Current != 0 || stats.RX.Average != 0 || stats.RX.Maximum != 0 || len(stats.RXHistory) != 0 {
		t.Fatalf("reset RX stats = %+v history=%v, want reset", stats.RX, stats.RXHistory)
	}
	if stats.RX.Total != 10 || stats.TX.Total != 20 {
		t.Fatalf("reset totals = RX %d TX %d, want 10 and 20", stats.RX.Total, stats.TX.Total)
	}
}

func TestSessionTotalSurvivesCounterReset(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 200)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 200, 400)))
	monitor.Update(snapshot(start.Add(2*time.Second), counters("eth0", 10, 20)))
	monitor.Update(snapshot(start.Add(3*time.Second), counters("eth0", 60, 80)))

	stats, _ := monitor.Stats("eth0")
	if stats.RX.SessionTotal != 150 || stats.TX.SessionTotal != 260 {
		t.Fatalf("session totals = RX %d TX %d, want 150 and 260", stats.RX.SessionTotal, stats.TX.SessionTotal)
	}
}

func TestResetStatsClearsTotalsAndDerivedValues(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 200)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 200, 400)))
	monitor.ResetStats()

	stats, _ := monitor.Stats("eth0")
	if stats.RX.Total != 0 || stats.TX.Total != 0 {
		t.Fatalf("totals after reset = RX %d TX %d, want zero", stats.RX.Total, stats.TX.Total)
	}
	if stats.RX.Current != 0 || stats.RX.Average != 0 || stats.RX.Maximum != 0 || stats.RX.SessionTotal != 0 || len(stats.RXHistory) != 0 {
		t.Fatalf("RX after reset = %+v history=%v, want cleared derived values", stats.RX, stats.RXHistory)
	}

	monitor.Update(snapshot(start.Add(2*time.Second), counters("eth0", 250, 500)))
	stats, _ = monitor.Stats("eth0")
	if stats.RX.Total != 50 || stats.TX.Total != 100 {
		t.Fatalf("totals after post-reset traffic = RX %d TX %d, want 50 and 100", stats.RX.Total, stats.TX.Total)
	}
}

func TestRebaseAvoidsPauseSpike(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 100)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 200, 200)))
	monitor.Rebase(snapshot(start.Add(10*time.Second), counters("eth0", 900, 900)))
	monitor.Update(snapshot(start.Add(11*time.Second), counters("eth0", 1000, 1000)))

	stats, _ := monitor.Stats("eth0")
	assertClose(t, stats.RX.Current, 100)
}

func TestDisappearanceAndReappearanceStartsNewGeneration(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 100)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 200, 200)))
	monitor.Update(snapshot(start.Add(2 * time.Second)))

	if got := monitor.Interfaces(); len(got) != 0 {
		t.Fatalf("Interfaces() after disappearance = %v, want empty", got)
	}
	monitor.Update(snapshot(start.Add(10*time.Second), counters("eth0", 900, 900)))
	stats, _ := monitor.Stats("eth0")
	if !stats.Available || stats.RX.Current != 0 || stats.RX.Average != 0 {
		t.Fatalf("reappeared stats = %+v, want fresh zero baseline", stats)
	}
}

func TestNonPositiveElapsedDoesNotCorruptBaseline(t *testing.T) {
	monitor := New(8)
	start := time.Unix(100, 0)
	monitor.Update(snapshot(start, counters("eth0", 100, 100)))
	monitor.Update(snapshot(start, counters("eth0", 200, 200)))
	monitor.Update(snapshot(start.Add(time.Second), counters("eth0", 300, 300)))
	stats, _ := monitor.Stats("eth0")
	assertClose(t, stats.RX.Current, 200)
	assertClose(t, stats.RX.Average, 200)
}

func snapshot(at time.Time, interfaces ...netstats.Counters) netstats.Snapshot {
	return netstats.Snapshot{At: at, Interfaces: interfaces}
}

func counters(name string, rx, tx uint64) netstats.Counters {
	return netstats.Counters{Name: name, RXBytes: rx, TXBytes: tx}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
