#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat >&2 <<'EOF'
Usage: publish-aur.sh <version> <aur-checkout> [options]

Required options:
  --maintainer <identity>  AUR maintainer in "Name <email>" form.

Optional:
  --package-name <name>      AUR package and executable name (default: netmeter-tui).
  --license <SPDX-ID>        License identifier (default: MIT).
  --repository <owner/repo>  GitHub repository (default: m-malanchuk/netmeter-tui).
  --publish                  Commit and push after all checks pass.
  -h, --help                 Show this help.
EOF
}

if (( $# == 1 )) && [[ "$1" == "-h" || "$1" == "--help" ]]; then
    usage
    exit 0
fi
if (( $# < 2 )); then
    usage
    exit 2
fi

version="${1#v}"
aur_checkout="$2"
shift 2

package_name="netmeter-tui"
license_id="MIT"
maintainer=""
repository="${NETMETER_TUI_GITHUB_REPOSITORY:-m-malanchuk/netmeter-tui}"
publish=false

while (( $# > 0 )); do
    case "$1" in
    --package-name)
        [[ $# -ge 2 ]] || { printf 'error: --package-name requires a value\n' >&2; exit 2; }
        package_name="$2"
        shift 2
        ;;
    --license)
        [[ $# -ge 2 ]] || { printf 'error: --license requires a value\n' >&2; exit 2; }
        license_id="$2"
        shift 2
        ;;
    --maintainer)
        [[ $# -ge 2 ]] || { printf 'error: --maintainer requires a value\n' >&2; exit 2; }
        maintainer="$2"
        shift 2
        ;;
    --repository)
        [[ $# -ge 2 ]] || { printf 'error: --repository requires a value\n' >&2; exit 2; }
        repository="$2"
        shift 2
        ;;
    --publish)
        publish=true
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

[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || {
    printf 'error: invalid release version %q\n' "${version}" >&2
    exit 2
}
[[ "${package_name}" =~ ^[a-z0-9@._+-]+$ ]] || {
    printf 'error: invalid or missing lowercase AUR package name\n' >&2
    exit 2
}
[[ "${license_id}" =~ ^[0-9A-Za-z.+-]+$ ]] || {
    printf 'error: invalid or missing SPDX license identifier\n' >&2
    exit 2
}
[[ -n "${maintainer}" && "${maintainer}" != *$'\n'* ]] || {
    printf 'error: invalid or missing maintainer identity\n' >&2
    exit 2
}
[[ "${repository}" =~ ^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+$ ]] || {
    printf 'error: invalid GitHub repository %q\n' "${repository}" >&2
    exit 2
}

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(CDPATH='' cd -- "${script_dir}/.." && pwd)"
template_dir="${project_dir}/packaging/aur"

[[ -f "${project_dir}/LICENSE" ]] || {
    printf 'error: add the selected project LICENSE before packaging\n' >&2
    exit 1
}
for command_name in curl git makepkg namcap sha256sum; do
    command -v "${command_name}" >/dev/null || {
        printf 'error: %s is required\n' "${command_name}" >&2
        exit 1
    }
done
git -C "${aur_checkout}" rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    printf 'error: %s is not an AUR Git checkout\n' "${aur_checkout}" >&2
    exit 1
}
if ! git -C "${aur_checkout}" remote -v | grep -Fq "aur.archlinux.org/${package_name}.git"; then
    printf 'error: AUR checkout remote does not match package %s\n' "${package_name}" >&2
    exit 1
fi
if [[ -n "$(git -C "${aur_checkout}" status --porcelain)" ]]; then
    printf 'error: AUR checkout has uncommitted changes\n' >&2
    exit 1
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dir}"' EXIT
source_archive="${temporary_dir}/${package_name}-${version}.tar.gz"
source_url="https://github.com/${repository}/archive/refs/tags/v${version}.tar.gz"

printf 'Downloading %s...\n' "${source_url}"
curl --fail --location --silent --show-error --output "${source_archive}" "${source_url}"
source_sha256="$(sha256sum "${source_archive}" | awk '{ print $1 }')"
source_dir="${repository##*/}-${version}"

escape_template_value() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//&/\\&}"
    value="${value//|/\\|}"
    printf '%s' "${value}"
}

render_template() {
    local input="$1"
    local output="$2"
    sed \
        -e "s|@PACKAGE_NAME@|$(escape_template_value "${package_name}")|g" \
        -e "s|@VERSION@|$(escape_template_value "${version}")|g" \
        -e "s|@REPOSITORY@|$(escape_template_value "${repository}")|g" \
        -e "s|@LICENSE@|$(escape_template_value "${license_id}")|g" \
        -e "s|@MAINTAINER@|$(escape_template_value "${maintainer}")|g" \
        -e "s|@SHA256@|$(escape_template_value "${source_sha256}")|g" \
        -e "s|@SOURCE_DIR@|$(escape_template_value "${source_dir}")|g" \
        "${input}" > "${output}"
}

render_template "${template_dir}/PKGBUILD.in" "${temporary_dir}/PKGBUILD"
render_template "${template_dir}/package.install.in" "${temporary_dir}/${package_name}.install"
install -m 0644 "${temporary_dir}/PKGBUILD" "${aur_checkout}/PKGBUILD"
install -m 0644 "${temporary_dir}/${package_name}.install" "${aur_checkout}/${package_name}.install"

(
    cd "${aur_checkout}"
    makepkg --printsrcinfo > .SRCINFO
    makepkg --cleanbuild --syncdeps
    namcap PKGBUILD
    while IFS= read -r package_file; do
        namcap "${package_file}"
    done < <(makepkg --packagelist)
    git diff --check
    git diff -- PKGBUILD .SRCINFO "${package_name}.install"
)

if [[ "${publish}" != true ]]; then
    printf 'AUR package prepared and verified. Review the diff, then rerun with --publish.\n'
    exit 0
fi

git -C "${aur_checkout}" add PKGBUILD .SRCINFO "${package_name}.install"
if git -C "${aur_checkout}" rev-parse --verify HEAD >/dev/null 2>&1; then
    commit_message="Update to ${version}"
else
    commit_message="Initial import: ${version}"
fi
git -C "${aur_checkout}" commit -m "${commit_message}"
git -C "${aur_checkout}" push origin HEAD:master
