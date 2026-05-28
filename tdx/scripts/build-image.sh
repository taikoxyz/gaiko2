#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../.." && pwd)
TDX_DIR="${REPO_ROOT}/tdx"
BUILD_DIR="${TDX_DIR}/build"
BIN_DIR="${BUILD_DIR}/bin"

require_file() {
    local name="$1"
    local path="$2"
    if [ ! -f "${path}" ]; then
        echo "${name} binary not found: ${path}" >&2
        exit 1
    fi
}

mkdir -p "${BIN_DIR}"

if [ -z "${GAIKO2_BIN:-}" ]; then
    GAIKO2_BIN="${BIN_DIR}/gaiko2"
    (cd "${REPO_ROOT}" && go build -trimpath -ldflags "-s -w -buildid=" -o "${GAIKO2_BIN}" ./cmd/gaiko2)
fi

: "${TAIKO_GETH_BIN:?TAIKO_GETH_BIN is required}"
: "${TAIKO_CLIENT_BIN:?TAIKO_CLIENT_BIN is required}"
: "${TDXS_BIN:?TDXS_BIN is required}"

require_file gaiko2 "${GAIKO2_BIN}"
require_file taiko-geth "${TAIKO_GETH_BIN}"
require_file taiko-client "${TAIKO_CLIENT_BIN}"
require_file tdxs "${TDXS_BIN}"

export GAIKO2_BIN
export TAIKO_GETH_BIN
export TAIKO_CLIENT_BIN
export TDXS_BIN

exec mkosi -C "${TDX_DIR}" build "$@"
