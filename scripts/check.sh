#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(CDPATH= cd -- "${script_dir}/.." && pwd)"
cd "${project_dir}"

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

bash -n scripts/*.sh packaging/aur/PKGBUILD.in packaging/aur/package.install.in
if command -v shellcheck >/dev/null; then
    shellcheck scripts/*.sh
fi

go mod verify
go test ./...
go vet ./...
go test -race ./...
go build -buildvcs=false ./...

printf 'All Go checks passed.\n'
