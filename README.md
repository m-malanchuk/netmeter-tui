# NetMeter TUI

[![CI](https://github.com/m-malanchuk/netmeter-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/m-malanchuk/netmeter-tui/actions/workflows/ci.yml)

A real-time network traffic monitor for the Linux terminal.

NetMeter TUI provides an `nload`-style interface dashboard and a process view
for inspecting TCP and UDP activity. Network statistics are read directly from
Linux interfaces such as `/proc/net/dev`; external tools like `ip`, `ifconfig`,
and `nload` are not required.

## Features

- Live incoming and outgoing transfer rates.
- Current, average, maximum, and total traffic values.
- Responsive RX and TX history graphs.
- Automatic network interface discovery.
- Sortable and filterable per-process traffic view.
- TCP and UDP connection counts.
- Adjustable sampling interval, pause, and reset controls.
- Six built-in color themes inspired by btop++.
- Graceful handling of interface and kernel counter resets.

## Requirements

- Linux.
- A terminal supported by [`tcell`](https://github.com/gdamore/tcell).
- Go 1.27 or newer when building from source.

The main interface dashboard works without elevated privileges. Process byte
accounting works best when the executable has `CAP_NET_ADMIN` and `CAP_NET_RAW`.
Without them, NetMeter TUI continues to work but may show connection counts
without TCP or UDP byte rates.

## Installation

### GitHub release

Download and inspect the installation script, then run it as your regular user:

```bash
curl -fsSLO https://raw.githubusercontent.com/m-malanchuk/netmeter-tui/main/scripts/install-release.sh
less install-release.sh
bash install-release.sh
```

The script installs NetMeter TUI to `/usr/local/bin` and configures the network
capabilities used by process mode. To install without those capabilities:

```bash
bash install-release.sh --no-capabilities
```

### Build from source

```bash
git clone https://github.com/m-malanchuk/netmeter-tui.git
cd netmeter-tui
go build -o netmeter-tui ./cmd/netmeter-tui
./netmeter-tui
```

For complete process accounting, grant the locally built executable the
required capabilities:

```bash
sudo setcap cap_net_admin,cap_net_raw+ep ./netmeter-tui
getcap ./netmeter-tui
```

Capabilities must be reapplied after rebuilding the executable.

## Usage

```bash
netmeter-tui [options]
```

Examples:

```bash
netmeter-tui
netmeter-tui -i eth0
netmeter-tui --interval 1s
netmeter-tui --show-loopback --theme nord
```

| Option | Default | Description |
| --- | --- | --- |
| `-i <interface>` | automatic | Monitor a specific network interface. |
| `--interval <duration>` | `500ms` | Set the initial sampling interval. |
| `--show-loopback` | disabled | Include the loopback interface. |
| `--theme <name>` | `adwaita-dark` | Select the initial color theme. |
| `--version` | — | Print the version and exit. |
| `-h`, `--help` | — | Show command help. |

The minimum interval is `50ms`. Available themes are `adwaita-dark`,
`gruvbox-dark`, `nord`, `dracula`, `tokyo-night`, and `solarized-dark`.

## Controls

### Dashboard

| Key | Action |
| --- | --- |
| Left / Up | Select the previous interface. |
| Right / Down | Select the next interface. |
| `p` / `F2` | Open process mode. |
| `+` / `=` | Increase the sampling interval. |
| `-` / `_` | Decrease the sampling interval. |
| Space | Pause or resume updates. |
| `r` | Reset rates, maxima, totals, and graph history. |
| `t` / `T` | Select the next or previous theme. |
| `q` / Ctrl+C | Exit. |

### Process mode

| Key | Action |
| --- | --- |
| Up / Down | Select a process. |
| Left / Right / Tab | Select the sort column. |
| Enter | Toggle the sort direction. |
| `,` / `.` | Select the previous or next interface. |
| `/` | Filter by process name or PID. |
| `p` / `F2` / Escape | Return to the dashboard. |

Pause, reset, interval, theme, and quit controls are available in both modes.

## Process mode limitations

Per-process traffic attribution is best-effort. TCP byte counters depend on
Linux `SOCK_DIAG`, while UDP byte rates use packet capture. Short-lived,
kernel-owned, namespace-isolated, or permission-hidden sockets may appear as
`unknown` or may not be attributed to a process.

Process totals represent observed socket payload and will not exactly match
interface totals from `/proc/net/dev`.

## License

NetMeter TUI is released under the [MIT License](LICENSE).
