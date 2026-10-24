#!/usr/bin/env bash

set -euo pipefail

repository="${NETMETER_TUI_GITHUB_REPOSITORY:-m-malanchuk/netmeter-tui}"
release_base_url="${NETMETER_TUI_RELEASE_BASE_URL:-}"
version="latest"
install_dir="/usr/local/bin"
grant_capabilities=true
install_capability_tools=false

usage() {
    cat <<'EOF'
Usage: install-release.sh [options]

Options:
  --version <version>       Install a tagged version instead of latest.
  --install-dir <path>      Install directory (default: /usr/local/bin).
  --no-capabilities         Do not grant CAP_BPF, CAP_NET_ADMIN, and CAP_NET_RAW.
  --install-capability-tools
                            Allow the script to install a missing setcap package.
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
    --install-capability-tools)
        install_capability_tools=true
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
    while IFS= read -r entry; do
        entry="${entry#./}"
        [[ -z "${entry}" ]] && continue
        case "${entry}" in
        /*|..|../*|*/../*)
            printf 'error: unsafe archive path %q\n' "${entry}" >&2
            exit 1
            ;;
        esac
    done < <(tar -tzf "${asset}")
    while IFS= read -r listing; do
        case "${listing:0:1}" in
        -|d) ;;
        *)
            printf 'error: archive contains a non-regular entry: %s\n' "${listing}" >&2
            exit 1
            ;;
        esac
    done < <(tar -tvzf "${asset}")
    mkdir extracted
    tar --extract --no-same-owner --no-same-permissions --file "${asset}" --directory extracted
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

target_path="${install_dir}/netmeter-tui"
previous_path="${temporary_dir}/previous-netmeter-tui"
had_previous=false
installation_changed=false

cleanup() {
    local status=$?
    if (( status != 0 )) && [[ "${installation_changed}" == true ]]; then
        if [[ "${had_previous}" == true ]]; then
            if ! run_as_root install -m 0755 "${previous_path}" "${target_path}"; then
                printf 'warning: failed to restore the previous installation at %s\n' "${target_path}" >&2
            fi
        elif ! run_as_root rm -f -- "${target_path}"; then
            printf 'warning: failed to remove the incomplete installation at %s\n' "${target_path}" >&2
        fi
    fi
    rm -rf -- "${temporary_dir}"
    exit "${status}"
}
trap cleanup EXIT

if [[ -L "${target_path}" ]]; then
    printf 'error: refusing to replace symlink at %s\n' "${target_path}" >&2
    exit 1
fi
if [[ -e "${target_path}" ]]; then
    run_as_root cp -a -- "${target_path}" "${previous_path}"
    had_previous=true
fi

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
    installation_changed=true
    install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
elif [[ ! -e "${install_dir}" && -w "$(dirname -- "${install_dir}")" ]]; then
    installation_changed=true
    install -d -m 0755 "${install_dir}"
    install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
else
    installation_changed=true
    run_as_root install -d -m 0755 "${install_dir}"
    run_as_root install -m 0755 "${binary}" "${install_dir}/netmeter-tui"
fi

if [[ "${grant_capabilities}" == true ]]; then
    if ! command -v setcap >/dev/null; then
        if [[ "${install_capability_tools}" == true ]]; then
            install_capability_tools
        else
            printf 'setcap is unavailable; leaving capabilities unchanged. Re-run with --install-capability-tools or --no-capabilities.\n' >&2
            grant_capabilities=false
        fi
    fi
fi

if [[ "${grant_capabilities}" == true ]]; then
    if run_as_root setcap 'cap_bpf,cap_net_admin,cap_net_raw=ep' "${target_path}"; then
        printf 'Granted CAP_BPF, CAP_NET_ADMIN, and CAP_NET_RAW for process traffic accounting.\n'
    elif run_as_root setcap 'cap_net_admin,cap_net_raw=ep' "${install_dir}/netmeter-tui"; then
        printf 'CAP_BPF is unavailable; falling back to packet-based UDP accounting.\n' >&2
    else
        printf 'Could not grant network capabilities; interface monitoring remains available.\n' >&2
    fi
fi

printf 'Installed %s\n' "${target_path}"
