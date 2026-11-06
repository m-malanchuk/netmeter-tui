package monitor

import (
	"math"
	"testing"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

func TestProcessMonitorFirstSampleAndElapsedRate(t *testing.T) {
	monitor := NewProcessMonitor()
	firstAt := time.Unix(10, 0)
	monitor.Update(procstats.Snapshot{At: firstAt, Processes: []procstats.ProcessCounters{{
		PID: 12, StartTime: 99, Name: "curl", Interface: "eth0", Connections: 1,
	}}})
	monitor.Update(procstats.Snapshot{At: firstAt.Add(2 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 12, StartTime: 99, Name: "curl", Interface: "eth0", RXBytes: 2048, TXBytes: 1024, Connections: 2,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].RX.Current != 1024 || rows[0].TX.Current != 512 {
		t.Fatalf("rates = %v/%v, want 1024/512", rows[0].RX.Current, rows[0].TX.Current)
	}
	if rows[0].Total != 3072 || rows[0].Connections != 2 {
		t.Fatalf("total/connections = %d/%d, want 3072/2", rows[0].Total, rows[0].Connections)
	}
}

func TestProcessMonitorSeparatesInterfacesAndReset(t *testing.T) {
	monitor := NewProcessMonitor()
	at := time.Unix(20, 0)
	monitor.Update(procstats.Snapshot{At: at, Processes: []procstats.ProcessCounters{
		{PID: 5, StartTime: 1, Name: "app", Interface: "eth0"},
		{PID: 5, StartTime: 1, Name: "app", Interface: "wlan0"},
	}})
	monitor.Update(procstats.Snapshot{At: at.Add(time.Second), Processes: []procstats.ProcessCounters{
		{PID: 5, StartTime: 1, Name: "app", Interface: "eth0", RXBytes: 10},
		{PID: 5, StartTime: 1, Name: "app", Interface: "wlan0", RXBytes: 20},
	}})
	if got := len(monitor.Stats("eth0")); got != 1 {
		t.Fatalf("eth0 rows = %d, want 1", got)
	}
	if got := monitor.Stats("wlan0")[0].Total; got != 20 {
		t.Fatalf("wlan0 total = %d, want 20", got)
	}
	monitor.ResetStats()
	row := monitor.Stats("eth0")[0]
	if row.Total != 0 || row.RX.Maximum != 0 || row.RX.Average != 0 {
		t.Fatalf("reset row = %+v", row)
	}
}

func TestProcessMonitorIncludesUnresolvedInterfaceRow(t *testing.T) {
	monitor := NewProcessMonitor()
	at := time.Unix(30, 0)
	monitor.Update(procstats.Snapshot{At: at, Processes: []procstats.ProcessCounters{{
		PID: 0, Name: "unknown", Connections: 1,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].Name != "unknown" {
		t.Fatalf("unknown rows = %+v", rows)
	}
}

func TestProcessMonitorNewProcessEstablishesBaseline(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(40, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 1, StartTime: 10, Name: "old", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 1, StartTime: 10, Name: "old", Interface: "eth0", RXBytes: 100,
		Connections: 1,
	}, {
		PID: 2, StartTime: 20, Name: "new", Interface: "eth0", RXBytes: 900,
		Connections: 1,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two processes", rows)
	}
	if rows[1].PID != 2 || rows[1].RX.Current != 0 || rows[1].Total != 0 {
		t.Fatalf("new process row = %+v, want zero baseline", rows[1])
	}
}

func TestProcessMonitorPIDReuseStartsFreshGeneration(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(50, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 7, StartTime: 100, Name: "old", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 7, StartTime: 100, Name: "old", Interface: "eth0", RXBytes: 50,
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(2 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 7, StartTime: 200, Name: "new", Interface: "eth0", RXBytes: 800,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].StartTime != 200 {
		t.Fatalf("rows after PID reuse = %+v, want only new generation", rows)
	}
	if rows[0].RX.Current != 0 || rows[0].Total != 0 {
		t.Fatalf("reused PID inherited traffic: %+v", rows[0])
	}
}

func TestProcessMonitorReappearanceStartsFreshGeneration(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(60, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 8, StartTime: 300, Name: "worker", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 8, StartTime: 300, Name: "worker", Interface: "eth0", RXBytes: 20,
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(2 * time.Second)})
	if rows := monitor.Stats("eth0"); len(rows) != 0 {
		t.Fatalf("rows after disappearance = %+v, want empty", rows)
	}
	monitor.Update(procstats.Snapshot{At: start.Add(10 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 8, StartTime: 300, Name: "worker", Interface: "eth0", RXBytes: 900,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].RX.Current != 0 || rows[0].Total != 0 {
		t.Fatalf("reappeared process = %+v, want fresh baseline", rows)
	}
}

func TestProcessMonitorRebaseAvoidsPauseSpike(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(70, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 9, StartTime: 400, Name: "worker", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 9, StartTime: 400, Name: "worker", Interface: "eth0", RXBytes: 100,
	}}})
	monitor.Rebase(start.Add(10 * time.Second))
	monitor.Update(procstats.Snapshot{At: start.Add(11 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 9, StartTime: 400, Name: "worker", Interface: "eth0", RXBytes: 100,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].RX.Current != 100 {
		t.Fatalf("post-rebase rate = %+v, want 100 bytes/s", rows)
	}
}

func TestProcessMonitorNonPositiveElapsedLeavesBaselineUntouched(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(80, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 10, StartTime: 500, Name: "worker", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 10, StartTime: 500, Name: "worker", Interface: "eth0", RXBytes: 100,
	}}})
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 10, StartTime: 500, Name: "worker", Interface: "eth0", RXBytes: 900,
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(2 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 10, StartTime: 500, Name: "worker", Interface: "eth0", RXBytes: 100,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].RX.Current != 100 || rows[0].Total != 200 {
		t.Fatalf("non-positive elapsed corrupted baseline: %+v", rows)
	}
}

func TestProcessMonitorSaturatesTotalsAndElapsed(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(90, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 11, StartTime: 600, Name: "worker", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 11, StartTime: 600, Name: "worker", Interface: "eth0", RXBytes: math.MaxUint64,
		TXBytes: math.MaxUint64,
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(2 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 11, StartTime: 600, Name: "worker", Interface: "eth0", RXBytes: 1,
		TXBytes: 1,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].RX.Total != math.MaxUint64 || rows[0].TX.Total != math.MaxUint64 || rows[0].Total != math.MaxUint64 {
		t.Fatalf("totals did not saturate: %+v", rows)
	}
}

func TestProcessMonitorZeroStartTimeUsesNameFallbackIdentity(t *testing.T) {
	monitor := NewProcessMonitor()
	start := time.Unix(100, 0)
	monitor.Update(procstats.Snapshot{At: start, Processes: []procstats.ProcessCounters{{
		PID: 12, Name: "old", Interface: "eth0",
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(time.Second), Processes: []procstats.ProcessCounters{{
		PID: 12, Name: "old", Interface: "eth0", RXBytes: 10,
	}}})
	monitor.Update(procstats.Snapshot{At: start.Add(2 * time.Second), Processes: []procstats.ProcessCounters{{
		PID: 12, Name: "new", Interface: "eth0", RXBytes: 100,
	}}})
	rows := monitor.Stats("eth0")
	if len(rows) != 1 || rows[0].Name != "new" || rows[0].Total != 0 {
		t.Fatalf("zero-start identity inherited old totals: %+v", rows)
	}
}
