// Package app wires collection, monitoring, and the terminal UI together.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/m-malanchuk/netmeter-tui/internal/monitor"
	"github.com/m-malanchuk/netmeter-tui/internal/netstats"
	"github.com/m-malanchuk/netmeter-tui/internal/procstats"
	"github.com/m-malanchuk/netmeter-tui/internal/ui"
)

const minimumInterval = 50 * time.Millisecond

var intervalPresets = [...]time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// Config contains runtime options for the application.
type Config struct {
	InterfaceName    string
	Interval         time.Duration
	ShowLoopback     bool
	Theme            string
	Collector        InterfaceCollector
	ProcessCollector ProcessCollector
}

// Run starts the terminal application and returns when the user exits or the
// context is canceled.
func Run(ctx context.Context, config Config) error {
	if config.Interval < minimumInterval {
		return fmt.Errorf("interval must be at least %s", minimumInterval)
	}
	if config.Theme == "" {
		config.Theme = "adwaita-dark"
	}
	if !ui.HasTheme(config.Theme) {
		return fmt.Errorf("unknown theme %q (available: %s)", config.Theme, strings.Join(ui.ThemeNames(), ", "))
	}
	collector := config.Collector
	if collector == nil {
		collector = netstats.NewCollector()
	}

	initial, err := collector.Collect(ctx)
	if err != nil {
		return err
	}
	if config.InterfaceName != "" && !hasInterface(initial.Interfaces, config.InterfaceName) {
		return fmt.Errorf("network interface %q was not found", config.InterfaceName)
	}
	processCollector := config.ProcessCollector
	if processCollector == nil {
		processCollector = procstats.NewCollector()
	}
	defer processCollector.Close()
	var processErr error
	var processWarning string
	processSampleReady := false

	screen, err := initializeScreen()
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	eventQuit := make(chan struct{})
	events := make(chan tcell.Event, 32)
	eventDone := make(chan struct{})
	interfaceSamples := make(chan sampleResult, 1)
	processSamples := make(chan sampleResult, 1)
	interfaceDone := make(chan struct{})
	processDone := make(chan struct{})
	intervalChanges := make(chan time.Duration, 1)
	processIntervalChanges := make(chan time.Duration, 1)
	processModeChanges := make(chan bool, 1)
	processRefreshes := make(chan struct{}, 1)
	go func() {
		screen.ChannelEvents(events, eventQuit)
		close(eventDone)
	}()
	defer func() {
		cancel()
		close(eventQuit)
		screen.Fini()
		<-eventDone
		<-interfaceDone
		<-processDone
	}()

	width, _ := screen.Size()
	monitorState := monitor.New(width)
	monitorState.Update(initial)
	processState := monitor.NewProcessMonitor()
	lastSnapshot := initial
	view := ui.New(screen, config.Interval, config.InterfaceName, config.Theme)
	currentInterval := config.Interval

	render := func(status string) {
		width, _ := screen.Size()
		monitorState.SetHistoryCapacity(width)
		view.SetInterfaces(visibleInterfaces(monitorState.Interfaces(), config.ShowLoopback))
		selected := view.Selected()
		view.SetProcesses(processState.Stats(selected))
		view.SetProcessLoading(view.ProcessMode() && !processSampleReady)
		stats, found := monitorState.Stats(selected)
		if !found {
			stats = monitor.InterfaceStats{Name: selected}
		}
		if view.ProcessMode() && status == "" {
			if !processSampleReady {
				status = "collecting process data..."
			} else if processErr != nil {
				status = "process mode unavailable: " + processErr.Error()
			} else if processWarning != "" {
				status = processWarning
			}
		}
		view.Render(stats, found, status)
	}
	render("")

	go func() {
		defer close(interfaceDone)
		interfaceSampleLoop(runCtx, collector, currentInterval, intervalChanges, interfaceSamples)
	}()
	go func() {
		defer close(processDone)
		processSampleLoop(runCtx, processCollector, currentInterval, processIntervalChanges, processModeChanges, processRefreshes, processSamples)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			action := view.HandleEvent(event)
			switch action {
			case ui.ActionQuit:
				return nil
			case ui.ActionTogglePause:
				processState.Rebase(time.Now())
				if !view.Paused() {
					status := monitorState.Rebase(lastSnapshot)
					render(status)
					continue
				}
				render("")
			case ui.ActionToggleProcess:
				if view.ProcessMode() {
					processSampleReady = false
				}
				publishLatest(processModeChanges, view.ProcessMode())
				render("")
			case ui.ActionPrevious, ui.ActionNext:
				if view.ProcessMode() {
					processSampleReady = false
					publishProcessRefresh(processRefreshes)
				}
				render("")
			case ui.ActionReset:
				monitorState.ResetStats()
				processState.ResetStats()
				render("statistics reset")
			case ui.ActionIntervalIncrease:
				currentInterval = adjustInterval(currentInterval, 1)
				view.SetInterval(currentInterval)
				publishLatest(intervalChanges, currentInterval)
				publishLatest(processIntervalChanges, currentInterval)
				render(fmt.Sprintf("sampling interval: %s", currentInterval))
			case ui.ActionIntervalDecrease:
				currentInterval = adjustInterval(currentInterval, -1)
				view.SetInterval(currentInterval)
				publishLatest(intervalChanges, currentInterval)
				publishLatest(processIntervalChanges, currentInterval)
				render(fmt.Sprintf("sampling interval: %s", currentInterval))
			case ui.ActionThemeNext, ui.ActionThemePrevious:
				render("theme: " + view.ThemeName())
			default:
				render("")
			}
		case result, ok := <-interfaceSamples:
			if !ok {
				return nil
			}
			if result.err != nil {
				render("collector error: " + result.err.Error())
				continue
			}
			lastSnapshot = result.snapshot
			if view.Paused() {
				continue
			}
			notice := monitorState.Update(result.snapshot)
			render(notice)
		case result, ok := <-processSamples:
			if !ok {
				return nil
			}
			processSampleReady = true
			if result.processErr == nil {
				processErr = nil
				processWarning = result.processSnapshot.Warning
				if !view.Paused() {
					processState.Update(result.processSnapshot)
				}
			} else {
				processErr = result.processErr
			}
			if view.ProcessMode() {
				render("")
			}
		}
	}
}

func adjustInterval(current time.Duration, direction int) time.Duration {
	if direction == 0 {
		return current
	}
	if direction > 0 {
		for _, preset := range intervalPresets {
			if preset > current {
				return preset
			}
		}
		return intervalPresets[len(intervalPresets)-1]
	}
	for index := len(intervalPresets) - 1; index >= 0; index-- {
		if intervalPresets[index] < current {
			return intervalPresets[index]
		}
	}
	return intervalPresets[0]
}

func visibleInterfaces(interfaces []monitor.InterfaceStats, showLoopback bool) []monitor.InterfaceStats {
	if showLoopback {
		return interfaces
	}
	visible := make([]monitor.InterfaceStats, 0, len(interfaces))
	for _, stats := range interfaces {
		if stats.Name != "lo" {
			visible = append(visible, stats)
		}
	}
	return visible
}

func hasInterface(interfaces []netstats.Counters, wanted string) bool {
	for _, counters := range interfaces {
		if counters.Name == wanted {
			return true
		}
	}
	return false
}
