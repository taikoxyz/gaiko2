#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../.." && pwd)

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

make_bin() {
    local name="$1"
    local path="${tmpdir}/${name}"
    printf '%s\n' "${name}-binary" >"${path}"
    chmod +x "${path}"
    printf '%s' "${path}"
}

out="${tmpdir}/manifest.json"

RELEASE_TAG=test-release \
OUT="${out}" \
GAIKO2_BIN="$(make_bin gaiko2)" \
TAIKO_GETH_BIN="$(make_bin taiko-geth)" \
TAIKO_CLIENT_BIN="$(make_bin taiko-client)" \
TDXS_BIN="$(make_bin tdxs)" \
TDX_IMAGE_ID=0xtestimage \
TDX_MRTD=0xtestmrtd \
"${REPO_ROOT}/tdx/scripts/export-manifest.sh"

assert_contains() {
    local needle="$1"
    if ! grep -Fq -- "${needle}" "${out}"; then
        echo "expected manifest to contain: ${needle}" >&2
        echo "--- manifest ---" >&2
        cat "${out}" >&2
        exit 1
    fi
}

assert_contains '"schema": "tdx-gaiko2-image-manifest-v1"'
assert_contains '"release_tag": "test-release"'
assert_contains '"name": "gaiko2"'
assert_contains '"name": "taiko-geth"'
assert_contains '"name": "taiko-client"'
assert_contains '"name": "tdxs"'
assert_contains '"path": "tdx/mkosi.extra/etc/systemd/system/gaiko2-tdxgeth.service"'
assert_contains '"path": "tdx/mkosi.extra/usr/local/bin/taiko-geth-tdx.sh"'
assert_contains '"tdx_image_id": "0xtestimage"'
assert_contains '"mrtd": "0xtestmrtd"'

echo "ok"
