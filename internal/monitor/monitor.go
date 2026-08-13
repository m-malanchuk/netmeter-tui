// Package monitor turns successive network counter snapshots into display data.
package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
)

// Metric contains the derived values for one traffic direction.
type Metric struct {
	Current      float64
	Average      float64
	Maximum      float64
	Total        uint64
	SessionTotal uint64
}

// InterfaceStats is the monitor's presentation-neutral view of one interface.
type InterfaceStats struct {
	Name      string
	Available bool
	RX        Metric
	TX        Metric
	RXHistory []float64
	TXHistory []float64
}

type interfaceState struct {
	name      string
	available bool
	lastAt    time.Time
	lastRX    uint64
	lastTX    uint64
	rx        directionState
	tx        directionState
}

type directionState struct {
	metric       Metric
	bytes        float64
	elapsed      time.Duration
	history      *History
	displayTotal uint64
	totalReset   bool
}

// Monitor stores per-interface state. Its methods are intentionally not
// internally synchronized; the application loop owns one Monitor instance.
type Monitor struct {
	states          map[string]*interfaceState
	historyCapacity int
}

// New creates a monitor retaining up to historyCapacity samples per direction.
func New(historyCapacity int) *Monitor {
	if historyCapacity < 0 {
		historyCapacity = 0
	}
	return &Monitor{
		states:          make(map[string]*interfaceState),
		historyCapacity: historyCapacity,
	}
}

// Update incorporates one snapshot. The caller must provide snapshots in
// chronological order.
func (m *Monitor) Update(snapshot netstats.Snapshot) string {
	notices := make([]string, 0)
	seen := make(map[string]struct{}, len(snapshot.Interfaces))
	for _, counters := range snapshot.Interfaces {
		seen[counters.Name] = struct{}{}
		state := m.states[counters.Name]
		if state == nil {
			state = m.newState(counters.Name)
			m.states[counters.Name] = state
		}

		if !state.available {
			state.startGeneration(counters, snapshot.At)
			continue
		}

		counterReset := counters.RXBytes < state.lastRX || counters.TXBytes < state.lastTX
		state.observe(counters, snapshot.At)
		if counterReset {
			notices = append(notices, fmt.Sprintf("counter reset detected on %s", counters.Name))
		}
	}

	for name, state := range m.states {
		if _, present := seen[name]; !present {
			state.available = false
		}
	}
	return strings.Join(notices, "; ")
}

// Rebase moves active baselines to snapshot without counting the elapsed pause
// interval as traffic. Existing aggregates are preserved unless the interface
// disappeared or a counter decreased while paused.
func (m *Monitor) Rebase(snapshot netstats.Snapshot) string {
	notices := make([]string, 0)
	seen := make(map[string]struct{}, len(snapshot.Interfaces))
	for _, counters := range snapshot.Interfaces {
		seen[counters.Name] = struct{}{}
		state := m.states[counters.Name]
		if state == nil {
			state = m.newState(counters.Name)
			m.states[counters.Name] = state
			state.startGeneration(counters, snapshot.At)
			continue
		}
		if !state.available || counters.RXBytes < state.lastRX || counters.TXBytes < state.lastTX {
			if state.available && (counters.RXBytes < state.lastRX || counters.TXBytes < state.lastTX) {
				notices = append(notices, fmt.Sprintf("counter reset detected on %s", counters.Name))
			}
			state.startGeneration(counters, snapshot.At)
			continue
		}

		state.available = true
		state.lastAt = snapshot.At
		state.lastRX = counters.RXBytes
		state.lastTX = counters.TXBytes
		state.rx.metric.Total = counters.RXBytes
		state.tx.metric.Total = counters.TXBytes
		if state.rx.totalReset {
			state.rx.metric.Total = state.rx.displayTotal
		}
		if state.tx.totalReset {
			state.tx.metric.Total = state.tx.displayTotal
		}
		state.rx.metric.Current = 0
		state.tx.metric.Current = 0
	}
	for name, state := range m.states {
		if _, present := seen[name]; !present {
			state.available = false
		}
	}
	return strings.Join(notices, "; ")
}

// ResetStats clears current, average, maximum, totals, session totals, and
// histories while retaining sampling baselines.
func (m *Monitor) ResetStats() {
	for _, state := range m.states {
		state.rx.resetDerived()
		state.tx.resetDerived()
	}
}

// Interfaces returns currently available interfaces in deterministic order.
func (m *Monitor) Interfaces() []InterfaceStats {
	names := make([]string, 0, len(m.states))
	for name, state := range m.states {
		if state.available {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	result := make([]InterfaceStats, 0, len(names))
	for _, name := range names {
		result = append(result, m.states[name].view())
	}
	return result
}

// Stats returns the retained state for name, including an unavailable state
// after an interface disappears. The boolean reports whether the name has ever
// been observed.
func (m *Monitor) Stats(name string) (InterfaceStats, bool) {
	state, ok := m.states[name]
	if !ok {
		return InterfaceStats{}, false
	}
	return state.view(), true
}

// SetHistoryCapacity changes the retained sample count for all interface states.
func (m *Monitor) SetHistoryCapacity(capacity int) {
	if capacity < 0 {
		capacity = 0
	}
	if m.historyCapacity == capacity {
		return
	}
	m.historyCapacity = capacity
	for _, state := range m.states {
		state.rx.history.Resize(capacity)
		state.tx.history.Resize(capacity)
	}
}

func (m *Monitor) newState(name string) *interfaceState {
	return &interfaceState{
		name: name,
		rx:   directionState{history: NewHistory(m.historyCapacity)},
		tx:   directionState{history: NewHistory(m.historyCapacity)},
	}
}

func (state *interfaceState) startGeneration(counters netstats.Counters, at time.Time) {
	state.available = true
	state.lastAt = at
	state.lastRX = counters.RXBytes
	state.lastTX = counters.TXBytes
	state.rx.reset(counters.RXBytes)
	state.tx.reset(counters.TXBytes)
}

func (state *interfaceState) observe(counters netstats.Counters, at time.Time) {
	state.available = true
	if !state.rx.totalReset {
		state.rx.metric.Total = counters.RXBytes
	}
	if !state.tx.totalReset {
		state.tx.metric.Total = counters.TXBytes
	}

	if counters.RXBytes < state.lastRX || counters.TXBytes < state.lastTX {
		state.startGeneration(counters, at)
		return
	}

	elapsed := at.Sub(state.lastAt)
	if elapsed <= 0 {
		state.rx.metric.Current = 0
		state.tx.metric.Current = 0
		return
	}

	rxDelta := counters.RXBytes - state.lastRX
	txDelta := counters.TXBytes - state.lastTX
	state.rx.addSample(rxDelta, elapsed)
	state.tx.addSample(txDelta, elapsed)
	state.rx.addTotal(rxDelta, counters.RXBytes)
	state.tx.addTotal(txDelta, counters.TXBytes)
	state.lastAt = at
	state.lastRX = counters.RXBytes
	state.lastTX = counters.TXBytes
}

func (state *interfaceState) view() InterfaceStats {
	return InterfaceStats{
		Name:      state.name,
		Available: state.available,
		RX:        state.rx.metric,
		TX:        state.tx.metric,
		RXHistory: state.rx.history.Values(),
		TXHistory: state.tx.history.Values(),
	}
}

func (state *directionState) reset(total uint64) {
	sessionTotal := state.metric.SessionTotal
	if !state.totalReset {
		state.displayTotal = total
	}
	state.metric = Metric{Total: state.displayTotal, SessionTotal: sessionTotal}
	state.bytes = 0
	state.elapsed = 0
	state.history.Clear()
}

func (state *directionState) resetDerived() {
	state.totalReset = true
	state.displayTotal = 0
	state.metric = Metric{}
	state.bytes = 0
	state.elapsed = 0
	state.history.Clear()
}

func (state *directionState) addSample(delta uint64, elapsed time.Duration) {
	seconds := elapsed.Seconds()
	rate := float64(delta) / seconds
	state.metric.Current = rate
	if rate > state.metric.Maximum {
		state.metric.Maximum = rate
	}
	state.metric.SessionTotal = saturatingAdd(state.metric.SessionTotal, delta)
	state.bytes += float64(delta)
	state.elapsed += elapsed
	state.metric.Average = state.bytes / state.elapsed.Seconds()
	state.history.Add(rate)
}

func (state *directionState) addTotal(delta, rawTotal uint64) {
	if !state.totalReset {
		state.displayTotal = rawTotal
	} else {
		state.displayTotal = saturatingAdd(state.displayTotal, delta)
	}
	state.metric.Total = state.displayTotal
}

func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}
