package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestParseArgsDefaults(t *testing.T) {
	config, showVersion, err := parseArgs(nil, "netmeter-tui", new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if showVersion {
		t.Fatal("default arguments unexpectedly request version")
	}
	if config.Interval != 500*time.Millisecond || config.Theme != "adwaita-dark" {
		t.Fatalf("defaults = interval %s, theme %q", config.Interval, config.Theme)
	}
}

func TestParseArgsFlags(t *testing.T) {
	config, showVersion, err := parseArgs([]string{"-version", "-i", "eth0", "-interval", "2s", "-show-loopback", "-theme", "nord"}, "netmeter-tui", new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	if !showVersion || config.InterfaceName != "eth0" || config.Interval != 2*time.Second || !config.ShowLoopback || config.Theme != "nord" {
		t.Fatalf("parsed config = %+v, version=%v", config, showVersion)
	}
}

func TestParseArgsHelp(t *testing.T) {
	var output bytes.Buffer
	_, _, err := parseArgs([]string{"-h"}, "netmeter-tui", &output)
	if err != flag.ErrHelp {
		t.Fatalf("help error = %v, want %v", err, flag.ErrHelp)
	}
	if !strings.Contains(output.String(), "Usage: netmeter-tui [options]") {
		t.Fatalf("help output = %q", output.String())
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	_, _, err := parseArgs([]string{"-unknown"}, "netmeter-tui", new(bytes.Buffer))
	if err == nil {
		t.Fatal("unknown flag unexpectedly accepted")
	}
}
