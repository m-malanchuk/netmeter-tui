#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(CDPATH='' cd -- "${script_dir}/.." && pwd)"
cd "${project_dir}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dir}"' EXIT

if [[ "$(go env GOOS)" != "linux" ]]; then
    printf 'error: netmeter-tui checks require Linux\n' >&2
    exit 1
fi

mapfile -t unformatted < <(gofmt -l cmd internal)
if (( ${#unformatted[@]} != 0 )); then
    printf 'error: gofmt is required for:\n' >&2
    printf '  %s\n' "${unformatted[@]}" >&2
    exit 1
fi

bash -n scripts/*.sh
bash scripts/install-release-test.sh
if command -v shellcheck >/dev/null; then
    shellcheck scripts/*.sh
else
    printf 'warning: shellcheck is unavailable; shell script lint was not run.\n' >&2
fi

go mod verify
go test ./...
go vet ./...
go test -race ./...
go build -buildvcs=false ./...

cli_binary="${temporary_dir}/netmeter-tui"
go build -buildvcs=false -o "${cli_binary}" ./cmd/netmeter-tui
help_output="$("${cli_binary}" --help 2>&1)"
[[ "${help_output}" == *"Usage:"* ]] || {
    printf 'error: built CLI help output is missing Usage\n' >&2
    exit 1
}
version_output="$("${cli_binary}" --version 2>&1)"
[[ "${version_output}" == "netmeter-tui dev" ]] || {
    printf 'error: built CLI version output is %q\n' "${version_output}" >&2
    exit 1
}
if "${cli_binary}" --unknown >/dev/null 2>&1; then
    printf 'error: built CLI accepted an unknown option\n' >&2
    exit 1
fi
invalid_interval_output="$("${cli_binary}" --interval 1ms 2>&1 || true)"
[[ "${invalid_interval_output}" == *"interval must be at least"* ]] || {
    printf 'error: invalid interval was not rejected by the built CLI\n' >&2
    exit 1
}
invalid_theme_output="$("${cli_binary}" --theme unknown 2>&1 || true)"
[[ "${invalid_theme_output}" == *"unknown theme"* ]] || {
    printf 'error: invalid theme was not rejected by the built CLI\n' >&2
    exit 1
}

run_optional_integration() {
    local label="$1"
    shift
    local output
    if ! output="$(go test "$@" 2>&1)"; then
        printf '%s\n' "${output}"
        return 1
    fi
    printf '%s\n' "${output}"
    if grep -q '^--- SKIP:' <<<"${output}"; then
        printf 'notice: %s was unavailable and skipped; this is not a passing integration result.\n' "${label}" >&2
    fi
}

if [[ "${NETMETER_TUI_PRIVILEGED_BPF:-0}" == "1" ]]; then
    run_optional_integration 'privileged UDP/eBPF integration' -run '^TestUDPBPFAttachmentIntegration$' -v ./internal/procstats
else
    printf 'notice: privileged UDP/eBPF integration was not run; set NETMETER_TUI_PRIVILEGED_BPF=1 to opt in.\n' >&2
fi

if [[ "${NETMETER_TUI_LIVE_TCP:-0}" == "1" ]]; then
    run_optional_integration 'live TCP attribution' -run '^TestTCPProcessAttributionIntegration$' -v ./internal/procstats
else
    printf 'notice: live TCP attribution was not run; set NETMETER_TUI_LIVE_TCP=1 to opt in.\n' >&2
fi

if [[ "${NETMETER_TUI_LIVE_UDP_PACKET:-0}" == "1" ]]; then
    run_optional_integration 'live UDP AF_PACKET capture' -run '^TestUDPAFPacketCaptureIntegration$' -v ./internal/procstats
else
    printf 'notice: live UDP AF_PACKET capture was not run; set NETMETER_TUI_LIVE_UDP_PACKET=1 to opt in.\n' >&2
fi

printf 'All non-privileged Go checks passed.\n'
