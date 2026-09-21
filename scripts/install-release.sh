#!/usr/bin/env bash

set -euo pipefail

repository="${NETMETER_TUI_GITHUB_REPOSITORY:-m-malanchuk/netmeter-tui}"
release_base_url="${NETMETER_TUI_RELEASE_BASE_URL:-}"
version="latest"
install_dir="/usr/local/bin"
grant_capabilities=true

usage() {
    cat <<'EOF'
Usage: install-release.sh [options]

Options:
  --version <version>       Install a tagged version instead of latest.
  --install-dir <path>      Install directory (default: /usr/local/bin).
  --no-capabilities         Do not grant CAP_NET_ADMIN and CAP_NET_RAW.
  -h, --help                Show this help.
EOF
}

while (( $# > 0 )); do
    case "$1" in
    --version)
        [[ $# -ge 2 ]] || { printf 'error: --version requires a value\n' >&2; exit 2; }
        version="${2#v}"
        shift 2
        ;;
    --install-dir)
        [[ $# -ge 2 ]] || { printf 'error: --install-dir requires a value\n' >&2; exit 2; }
        install_dir="$2"
        shift 2
        ;;
    --no-capabilities)
        grant_capabilities=false
        shift
        ;;
    -h|--help)
        usage
        exit 0
        ;;
    *)
        printf 'error: unknown option %q\n' "$1" >&2
        usage
        exit 2
        ;;
    esac
done

if [[ ! "${repository}" =~ ^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+$ ]]; then
    printf 'error: invalid GitHub repository %q\n' "${repository}" >&2
    exit 2
fi
if [[ "${version}" != "latest" && ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf 'error: invalid release version %q\n' "${version}" >&2
    exit 2
fi
if [[ "${install_dir}" != /* || "${install_dir}" == "/" ]]; then
    printf 'error: --install-dir must be an absolute directory other than /\n' >&2
    exit 2
fi

case "$(uname -m)" in
x86_64|amd64) architecture="amd64" ;;
aarch64|arm64) architecture="arm64" ;;
*)
    printf 'error: unsupported architecture %q\n' "$(uname -m)" >&2
    exit 1
    ;;
esac

command -v curl >/dev/null || { printf 'error: curl is required\n' >&2; exit 1; }
command -v sha256sum >/dev/null || { printf 'error: sha256sum is required\n' >&2; exit 1; }
command -v tar >/dev/null || { printf 'error: tar is required\n' >&2; exit 1; }

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dir}"' EXIT

asset="netmeter-tui_linux_${architecture}.tar.gz"
if [[ -n "${release_base_url}" ]]; then
    release_url="${release_base_url%/}"
elif [[ "${version}" == "latest" ]]; then
    release_url="https://github.com/${repository}/releases/latest/download"
else
    release_url="https://github.com/${repository}/releases/download/v${version}"
fi

printf 'Downloading netmeter-tui %s for linux/%s...\n' "${version}" "${architecture}"
curl --fail --location --silent --show-error \
    --output "${temporary_dir}/${asset}" \
    "${release_url}/${asset}"
curl --fail --location --silent --show-error \
    --output "${temporary_dir}/checksums.txt" \
    "${release_url}/checksums.txt"

(
    cd "${temporary_dir}"
    expected="$(awk -v file="${asset}" '$2 == file || $2 == ("*" file) { print; exit }' checksums.txt)"
    if [[ -z "${expected}" ]]; then
        printf 'error: release checksum for %s is missing\n' "${asset}" >&2
        exit 1
    fi
    printf '%s\n' "${expected}" | sha256sum --check --strict -
    mkdir extracted
    tar -xzf "${asset}" -C extracted
)

binary="${temporary_dir}/extracted/netmeter-tui"
[[ -x "${binary}" ]] || { printf 'error: release archive does not contain netmeter-tui\n' >&2; exit 1; }

run_as_root() {
    if (( EUID == 0 )); then
        "$@"
        return
    fi
    command -v sudo >/dev/null || { printf 'error: sudo is required for system installation\n' >&2; exit 1; }
    sudo "$@"
}

install_capability_tools() {
    printf 'setcap is not installed; installing the platform capability tools...\n'
    if command -v pacman >/dev/null; then
        run_as_root pacman -S --needed --noconfirm libcap
    elif command -v apt-get >/dev/null; then
        run_as_root apt-get update
        run_as_root apt-get install -y libcap2-bin
    elif command -v dnf >/dev/null; then
        run_as_root dnf install -y libcap
    elif command -v zypper >/dev/null; then
        run_as_root zypper --non-interactive install libcap-progs
    elif command -v apk >/dev/null; then
        run_as_root apk add libcap
    else
        printf 'error: setcap is missing and the package manager is unsupported\n' >&2
        exit 1
    fi
}

if [[ -d "${install_dir}" && -w "${install_dir}" ]]; then
    install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
elif [[ ! -e "${install_dir}" && -w "$(dirname -- "${install_dir}")" ]]; then
    install -d -m 0755 "${install_dir}"
    install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
else
    run_as_root install -d -m 0755 "${install_dir}"
    run_as_root install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
fi

if [[ "${grant_capabilities}" == true ]]; then
    command -v setcap >/dev/null || install_capability_tools
    run_as_root setcap 'cap_net_admin,cap_net_raw=ep' "${install_dir}/netmeter-tui"
fi

printf 'Installed %s\n' "${install_dir}/netmeter-tui"
if [[ "${grant_capabilities}" == true ]]; then
    printf 'Granted CAP_NET_ADMIN and CAP_NET_RAW for process traffic accounting.\n'
fi
