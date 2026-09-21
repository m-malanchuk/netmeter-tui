#!/usr/bin/env bash

set -euo pipefail

usage() {
    printf 'Usage: %s <version> [--publish]\n' "${0##*/}" >&2
    printf 'The default creates a draft GitHub release.\n' >&2
}

if (( $# == 1 )) && [[ "$1" == "-h" || "$1" == "--help" ]]; then
	usage
	exit 0
fi
if (( $# < 1 || $# > 2 )); then
    usage
    exit 2
fi

version="${1#v}"
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf 'error: invalid release version %q\n' "${1}" >&2
    exit 2
fi
publish=false
if (( $# == 2 )); then
    [[ "$2" == "--publish" ]] || { usage; exit 2; }
    publish=true
fi

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(CDPATH='' cd -- "${script_dir}/.." && pwd)"
cd "${project_dir}"

command -v gh >/dev/null || { printf 'error: GitHub CLI (gh) is required\n' >&2; exit 1; }
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    printf 'error: the project is not a Git worktree\n' >&2
    exit 1
}
[[ -f LICENSE ]] || {
    printf 'error: add the selected project LICENSE before publishing\n' >&2
    exit 1
}
if [[ -n "$(git status --porcelain)" ]]; then
    printf 'error: commit or remove working-tree changes before publishing\n' >&2
    exit 1
fi
git rev-parse --verify --quiet "refs/tags/v${version}" >/dev/null || {
    printf 'error: tag v%s does not exist\n' "${version}" >&2
    exit 1
}
if [[ "$(git rev-parse "refs/tags/v${version}^{commit}")" != "$(git rev-parse HEAD)" ]]; then
    printf 'error: tag v%s does not point to HEAD\n' "${version}" >&2
    exit 1
fi

"${script_dir}/check.sh"
"${script_dir}/build-release.sh" "${version}"

release_options=(--verify-tag --title "v${version}" --generate-notes)
if [[ "${publish}" != true ]]; then
    release_options+=(--draft)
fi

gh release create "v${version}" \
    "${project_dir}/dist/v${version}/netmeter-tui_linux_amd64.tar.gz" \
    "${project_dir}/dist/v${version}/netmeter-tui_linux_arm64.tar.gz" \
    "${project_dir}/dist/v${version}/checksums.txt" \
    "${release_options[@]}"
