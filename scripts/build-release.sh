#!/usr/bin/env bash

set -euo pipefail

usage() {
    printf 'Usage: %s <version>\n' "${0##*/}" >&2
    printf 'Example: %s 0.1.0\n' "${0##*/}" >&2
}

if (( $# == 1 )) && [[ "$1" == "-h" || "$1" == "--help" ]]; then
	usage
	exit 0
fi
if (( $# != 1 )); then
    usage
    exit 2
fi

version="${1#v}"
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf 'error: invalid release version %q\n' "${1}" >&2
    exit 2
fi

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(CDPATH= cd -- "${script_dir}/.." && pwd)"
output_dir="${project_dir}/dist/v${version}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dir}"' EXIT

mkdir -p "${output_dir}"

targets=(amd64 arm64)
assets=()
for architecture in "${targets[@]}"; do
    archive="netmeter-tui_linux_${architecture}.tar.gz"
    staging_dir="${temporary_dir}/netmeter-tui_linux_${architecture}"
    mkdir -p "${staging_dir}"

    printf 'Building linux/%s...\n' "${architecture}"
    env \
        CGO_ENABLED=0 \
        GOOS=linux \
        GOARCH="${architecture}" \
        go build \
            -buildvcs=false \
            -trimpath \
            -ldflags="-s -w -X main.version=${version}" \
            -o "${staging_dir}/netmeter-tui" \
            ./cmd/netmeter-tui

    install -m 0644 README.md "${staging_dir}/README.md"
    if [[ -f LICENSE ]]; then
        install -m 0644 LICENSE "${staging_dir}/LICENSE"
    fi
    tar -C "${staging_dir}" -czf "${output_dir}/${archive}" .
    assets+=("${archive}")
done

(
    cd "${output_dir}"
    sha256sum "${assets[@]}" > checksums.txt
)

printf 'Release artifacts: %s\n' "${output_dir}"
