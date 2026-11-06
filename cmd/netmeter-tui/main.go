package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/m-malanchuk/netmeter-tui/internal/app"
)

var version = "dev"

func main() {
	config, showVersion, err := parseArgs(os.Args[1:], os.Args[0], os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, "netmeter-tui:", err)
		os.Exit(2)
	}
	if showVersion {
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

func parseArgs(args []string, commandName string, output io.Writer) (app.Config, bool, error) {
	config := app.Config{Interval: 500 * time.Millisecond, Theme: "adwaita-dark"}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(output)
	showVersion := false
	flags.BoolVar(&showVersion, "version", false, "print version and exit")
	flags.StringVar(&config.InterfaceName, "i", "", "network interface to monitor")
	flags.DurationVar(&config.Interval, "interval", config.Interval, "sampling interval (for example, 500ms or 2s)")
	flags.BoolVar(&config.ShowLoopback, "show-loopback", false, "include the loopback interface lo")
	flags.StringVar(&config.Theme, "theme", config.Theme, "color theme: adwaita-dark, gruvbox-dark, nord, dracula, tokyo-night, or solarized-dark")
	flags.Usage = func() {
		fmt.Fprintf(output, "Usage: %s [options]\n\n", commandName)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return app.Config{}, false, err
	}
	return config, showVersion, nil
}
