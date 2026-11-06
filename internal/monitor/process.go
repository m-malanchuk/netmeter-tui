package monitor

import (
	"sort"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
)

// ProcessStats is presentation-neutral data for one process in the process
// view. RX and TX totals are observed application/socket bytes, not interface
// counters.
type ProcessStats struct {
	PID         int
	StartTime   uint64
	Name        string
	Interface   string
	RX          Metric
	TX          Metric
	Total       uint64
	Connections int
}

type processStateKey struct {
	pid           int
	startTime     uint64
	interfaceName string
	// A zero start time means procfs could not provide a generation identity.
	// Include the observed name in that fallback key so a PID with unavailable
	// generation data cannot silently inherit another process's totals.
	name string
}

type processState struct {
	stats   ProcessStats
	elapsed time.Duration
}

// ProcessMonitor turns interval process observations into rates and observed
// totals. It is owned by the application event loop and is not synchronized.
type ProcessMonitor struct {
	states map[processStateKey]*processState
	lastAt time.Time
}

// NewProcessMonitor creates an empty process monitor.
func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{states: make(map[processStateKey]*processState)}
}

// Update incorporates one process snapshot. ProcessCounters contain deltas
// since the previous collector call, so a first observation (including a
// process appearing after startup) always establishes a baseline and does not
// create a startup spike. A process missing from a snapshot is removed; a later
// reappearance starts a fresh generation.
func (m *ProcessMonitor) Update(snapshot procstats.Snapshot) {
	if snapshot.At.IsZero() {
		return
	}
	if m.lastAt.IsZero() {
		m.lastAt = snapshot.At
		m.replaceUnavailable(snapshot)
		return
	}
	elapsed := snapshot.At.Sub(m.lastAt)
	if elapsed <= 0 {
		return
	}
	m.lastAt = snapshot.At
	seen := make(map[processStateKey]struct{}, len(snapshot.Processes))
	for _, counters := range snapshot.Processes {
		key := processKey(counters)
		seen[key] = struct{}{}
		state := m.states[key]
		if state == nil {
			state = &processState{stats: ProcessStats{
				PID:       counters.PID,
				StartTime: counters.StartTime,
				Name:      counters.Name,
				Interface: counters.Interface,
			}}
			m.states[key] = state
			state.stats.Connections = counters.Connections
			continue
		}
		state.stats.Name = counters.Name
		state.stats.Interface = counters.Interface
		state.stats.Connections = counters.Connections
		state.stats.RX.addSample(counters.RXBytes, elapsed)
		state.stats.TX.addSample(counters.TXBytes, elapsed)
		state.stats.RX.addTotal(counters.RXBytes)
		state.stats.TX.addTotal(counters.TXBytes)
		state.elapsed = saturatingDurationAdd(state.elapsed, elapsed)
		seconds := state.elapsed.Seconds()
		if seconds > 0 {
			state.stats.RX.Average = float64(state.stats.RX.SessionTotal) / seconds
			state.stats.TX.Average = float64(state.stats.TX.SessionTotal) / seconds
		}
		state.stats.Total = saturatingAdd(state.stats.RX.Total, state.stats.TX.Total)
	}
	for key := range m.states {
		if _, present := seen[key]; !present {
			delete(m.states, key)
		}
	}
}

// Rebase discards the elapsed pause interval for process rates.
func (m *ProcessMonitor) Rebase(at time.Time) {
	if at.IsZero() {
		return
	}
	m.lastAt = at
	for _, state := range m.states {
		state.stats.RX.Current = 0
		state.stats.TX.Current = 0
	}
}

// ResetStats clears rates, maxima, observed totals, and keeps process
// identities/connections available for the next sample.
func (m *ProcessMonitor) ResetStats() {
	for _, state := range m.states {
		connections := state.stats.Connections
		state.stats.RX = Metric{}
		state.stats.TX = Metric{}
		state.stats.Total = 0
		state.stats.Connections = connections
		state.elapsed = 0
	}
}

// Stats returns processes belonging to interfaceName in deterministic order.
func (m *ProcessMonitor) Stats(interfaceName string) []ProcessStats {
	result := make([]ProcessStats, 0, len(m.states))
	for _, state := range m.states {
		if interfaceName != "" && state.stats.Interface != interfaceName && state.stats.Interface != "" {
			continue
		}
		result = append(result, state.stats)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PID != result[j].PID {
			return result[i].PID < result[j].PID
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (m *ProcessMonitor) replaceUnavailable(snapshot procstats.Snapshot) {
	for _, counters := range snapshot.Processes {
		key := processKey(counters)
		m.states[key] = &processState{stats: ProcessStats{
			PID:         counters.PID,
			StartTime:   counters.StartTime,
			Name:        counters.Name,
			Interface:   counters.Interface,
			Connections: counters.Connections,
		}}
	}
}

func processKey(counters procstats.ProcessCounters) processStateKey {
	key := processStateKey{pid: counters.PID, startTime: counters.StartTime, interfaceName: counters.Interface}
	if counters.StartTime == 0 {
		key.name = counters.Name
	}
	return key
}

func (metric *Metric) addSample(delta uint64, elapsed time.Duration) {
	if elapsed <= 0 {
		metric.Current = 0
		return
	}
	rate := float64(delta) / elapsed.Seconds()
	metric.Current = rate
	if rate > metric.Maximum {
		metric.Maximum = rate
	}
	metric.SessionTotal = saturatingAdd(metric.SessionTotal, delta)
}

func (metric *Metric) addTotal(delta uint64) {
	metric.Total = saturatingAdd(metric.Total, delta)
}

func saturatingDurationAdd(current, delta time.Duration) time.Duration {
	if delta <= 0 {
		return current
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if current > maxDuration-delta {
		return maxDuration
	}
	return current + delta
}
