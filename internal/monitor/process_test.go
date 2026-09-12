package monitor

import (
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
