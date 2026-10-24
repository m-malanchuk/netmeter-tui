#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
installer="${script_dir}/install-release.sh"
temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "${temporary_dir}"' EXIT

release_dir="${temporary_dir}/release"
stage_dir="${temporary_dir}/stage"
install_dir="${temporary_dir}/bin"
mkdir -p "${release_dir}" "${stage_dir}"

printf '#!/bin/sh\nprintf test-binary\n' > "${stage_dir}/netmeter-tui"
printf 'fixture README\n' > "${stage_dir}/README.md"
chmod 0755 "${stage_dir}/netmeter-tui"

asset="netmeter-tui_linux_amd64.tar.gz"
tar -C "${stage_dir}" -czf "${release_dir}/${asset}" .
(cd "${release_dir}" && sha256sum "${asset}" > checksums.txt)

NETMETER_TUI_RELEASE_BASE_URL="file://${release_dir}" \
    bash "${installer}" \
        --version 0.2.0 \
        --install-dir "${install_dir}" \
        --no-capabilities

[[ -x "${install_dir}/netmeter-tui" ]]
[[ "$("${install_dir}/netmeter-tui")" == "test-binary" ]]

if NETMETER_TUI_RELEASE_BASE_URL="file://${release_dir}" \
    bash "${installer}" --install-dir relative --no-capabilities; then
    printf 'error: installer accepted a relative install directory\n' >&2
    exit 1
fi

bad_release_dir="${temporary_dir}/bad-release"
bad_stage_dir="${temporary_dir}/bad-stage"
mkdir -p "${bad_release_dir}" "${bad_stage_dir}"
ln -s /tmp/netmeter-tui-outside "${bad_stage_dir}/netmeter-tui"
tar -C "${bad_stage_dir}" -czf "${bad_release_dir}/${asset}" .
(cd "${bad_release_dir}" && sha256sum "${asset}" > checksums.txt)
if NETMETER_TUI_RELEASE_BASE_URL="file://${bad_release_dir}" \
    bash "${installer}" --version 0.2.0 --install-dir "${install_dir}" --no-capabilities; then
    printf 'error: installer accepted an archive symlink\n' >&2
    exit 1
fi
[[ "$("${install_dir}/netmeter-tui")" == "test-binary" ]]

printf 'Release installer integration checks passed.\n'
