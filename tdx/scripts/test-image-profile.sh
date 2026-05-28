#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../.." && pwd)

assert_file() {
    local path="$1"
    if [ ! -f "${REPO_ROOT}/${path}" ]; then
        echo "missing file: ${path}" >&2
        exit 1
    fi
}

assert_executable() {
    local path="$1"
    assert_file "${path}"
    if [ ! -x "${REPO_ROOT}/${path}" ]; then
        echo "not executable: ${path}" >&2
        exit 1
    fi
}

assert_contains() {
    local path="$1"
    local pattern="$2"
    if ! grep -Fq -- "${pattern}" "${REPO_ROOT}/${path}"; then
        echo "expected ${path} to contain: ${pattern}" >&2
        exit 1
    fi
}

assert_contains tdx/mkosi.conf "ManifestFormat=json"
assert_contains tdx/mkosi.conf "SourceDateEpoch=0"
assert_contains tdx/mkosi.conf "KernelCommandLine="
assert_contains tdx/mkosi.conf "BuildScripts=mkosi.build"
assert_contains tdx/mkosi.conf "PostInstallationScripts=mkosi.postinst"

assert_executable tdx/mkosi.build
assert_executable tdx/mkosi.postinst
assert_executable tdx/mkosi.extra/usr/local/bin/runtime-init
assert_executable tdx/scripts/build-image.sh

assert_file tdx/mkosi.extra/etc/systemd/system/runtime-init.service
assert_contains tdx/mkosi.extra/etc/systemd/system/runtime-init.service "ExecStart=/usr/local/bin/runtime-init"
assert_contains tdx/mkosi.extra/etc/systemd/system/tdxs.service "Requires=runtime-init.service"
assert_contains tdx/mkosi.extra/etc/systemd/system/taiko-geth.service "Requires=runtime-init.service"
assert_contains tdx/mkosi.extra/etc/systemd/system/gaiko2-tdxgeth.service "Requires=runtime-init.service tdxs.service taiko-geth.service"
assert_contains tdx/mkosi.postinst "runtime-init.service"
assert_contains tdx/mkosi.postinst "gaiko2-tdxgeth.service"
assert_contains tdx/mkosi.postinst "systemctl enable"

echo "ok"
