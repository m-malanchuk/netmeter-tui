package app

import "time"

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

// Config contains runtime options and dependency seams for the application.
// CLI parsing remains in cmd/netmeter-tui; this type is not a persistent config
// file format.
type Config struct {
	InterfaceName    string
	Interval         time.Duration
	ShowLoopback     bool
	Theme            string
	Collector        InterfaceCollector
	ProcessCollector ProcessCollector
	ScreenFactory    ScreenFactory
}
