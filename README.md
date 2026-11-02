# NetMeter TUI

[![CI](https://github.com/m-malanchuk/netmeter-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/m-malanchuk/netmeter-tui/actions/workflows/ci.yml)

NetMeter TUI is a Linux terminal monitor for live network traffic by interface,
with an optional best-effort view of traffic attributed to TCP and UDP processes.

## Features

- RX and TX rates for discovered network interfaces.
- Current, average, maximum, session total, and kernel counter values.
- Bounded RX/TX history graphs that adapt to terminal size.
- Deterministic interface selection that survives interface changes.
- Optional process view with filtering, sorting, connection counts, and metric bars.
- TCP byte observations through Linux socket diagnostics when permitted.
- UDP byte observations through eBPF when available, with AF_PACKET fallback.
- Pause, reset, interval, interface, and theme controls.
- Six built-in color themes.

Process attribution is intentionally best effort. Process totals measure observed
socket traffic and are not expected to equal `/proc/net/dev` interface totals.

## Requirements

- Linux.
- A terminal supported by [tcell](https://github.com/gdamore/tcell).
- Go 1.27 or newer when building from source.

The interface dashboard reads `/proc/net/dev` and works without elevated
privileges. Process connection rows may still be visible without extra
privileges, but TCP and UDP byte rates can be unavailable. Complete process
accounting may require `CAP_NET_ADMIN`, `CAP_NET_RAW`, and, for eBPF UDP
accounting, `CAP_BPF`, depending on the kernel and security policy.

## Installation

### Build from source

```bash
git clone https://github.com/m-malanchuk/netmeter-tui.git
cd netmeter-tui
go build -buildvcs=false -o netmeter-tui ./cmd/netmeter-tui
./netmeter-tui
```

The source build is the verified installation path in the current working tree.
Capabilities must be configured separately if process byte accounting is needed:

```bash
sudo setcap 'cap_bpf,cap_net_admin,cap_net_raw=ep' ./netmeter-tui
getcap ./netmeter-tui
```

If the target system does not support `CAP_BPF`, use the capabilities supported
by that system; NetMeter TUI will attempt its packet-based fallback when possible.
Reapply file capabilities after rebuilding the executable.

### Release installation

For a tagged GitHub release, Linux archives are produced for `amd64` and
`arm64`. Before using the installer, inspect the script and verify that the
selected release is the one you intend to install:

```bash
curl -fsSLO https://raw.githubusercontent.com/m-malanchuk/netmeter-tui/main/scripts/install-release.sh
less install-release.sh
bash install-release.sh --version 0.2.0
```

The installer verifies the downloaded archive against the release checksums. Use
`--no-capabilities` when only the interface dashboard is required.
It does not invoke a system package manager unless you explicitly pass
`--install-capability-tools`.

## Usage

```text
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
| `-i <interface>` | automatic | Monitor one named interface. It must exist at startup. |
| `--interval <duration>` | `500ms` | Set the initial sampling interval. The minimum is `50ms`. |
| `--show-loopback` | disabled | Include the `lo` interface in automatic discovery. |
| `--theme <name>` | `adwaita-dark` | Select the initial built-in theme. |
| `--version` | — | Print the executable version and exit. |
| `-h`, `--help` | — | Show command help. |

Available themes are `adwaita-dark`, `gruvbox-dark`, `nord`, `dracula`,
`tokyo-night`, and `solarized-dark`.

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
| Enter | Toggle sort direction. |
| `,` / `.` | Select the previous or next interface. |
| `/` | Filter by process name or PID. |
| `p` / `F2` / Escape | Return to the dashboard. |

Pause, reset, interval, theme, and quit controls are shared by both views.

## Process-mode limitations

Process mode depends on information exposed by the running Linux kernel and
procfs permissions. A socket can be hidden, close between collection steps, or
belong to a namespace that cannot be resolved by the current process. Such rows
may be shown as `unknown` or omitted from byte attribution.

When eBPF is unavailable, NetMeter TUI attempts best-effort AF_PACKET UDP
observation. If neither mechanism is available, UDP connection rows can remain
visible while their byte rates stay zero and the UI reports the limitation.

## License

NetMeter TUI is distributed under the [MIT License](LICENSE).
