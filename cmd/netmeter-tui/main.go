package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/app"
)

var version = "dev"

func main() {
	var config app.Config
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.StringVar(&config.InterfaceName, "i", "", "network interface to monitor")
	flag.DurationVar(&config.Interval, "interval", 500*time.Millisecond, "sampling interval (for example, 500ms or 2s)")
	flag.BoolVar(&config.ShowLoopback, "show-loopback", false, "include the loopback interface lo")
	flag.StringVar(&config.Theme, "theme", "adwaita-dark", "color theme: adwaita-dark, gruvbox-dark, nord, dracula, tokyo-night, or solarized-dark")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Printf("netmeter-tui %s\n", version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, config); err != nil {
		fmt.Fprintln(os.Stderr, "netmeter-tui:", err)
		os.Exit(1)
	}
}
