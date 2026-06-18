#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)

export GAIKO2_PROVING_MODE="${GAIKO2_PROVING_MODE:-tee}"
export GAIKO2_TEE_TYPE="${GAIKO2_TEE_TYPE:-tdx}"
export GAIKO2_TDXS_SOCKET="${GAIKO2_TDXS_SOCKET:-/var/tdxs.sock}"
export GAIKO2_CONFIG_DIR="${GAIKO2_CONFIG_DIR:-${HOME}/.config/gaiko2/tdx/config}"
export GAIKO2_SECRET_DIR="${GAIKO2_SECRET_DIR:-${HOME}/.config/gaiko2/tdx/secrets}"
export GAIKO2_PORT="${GAIKO2_PORT:-8080}"

if [[ "${GAIKO2_PROVING_MODE}" != "tee" ]]; then
    echo "error: GAIKO2_PROVING_MODE must be tee for TDX replay" >&2
    exit 1
fi
if [[ "${GAIKO2_TEE_TYPE}" != "tdx" ]]; then
    echo "error: GAIKO2_TEE_TYPE must be tdx for TDX replay" >&2
    exit 1
fi
if [[ -z "${GAIKO2_INSTANCE_ID:-}" && -z "${GAIKO2_FORK:-}" ]]; then
    echo "error: set GAIKO2_INSTANCE_ID or GAIKO2_FORK before starting TDX replay" >&2
    exit 1
fi
if [[ ! -S "${GAIKO2_TDXS_SOCKET}" ]]; then
    echo "error: TDXS socket not found: ${GAIKO2_TDXS_SOCKET}" >&2
    exit 1
fi

mkdir -p "${GAIKO2_CONFIG_DIR}" "${GAIKO2_SECRET_DIR}"

if [[ -n "${GAIKO2_BIN:-}" ]]; then
    GAIKO2_CMD=("${GAIKO2_BIN}")
elif [[ -x "${REPO_ROOT}/bin/gaiko2" ]]; then
    GAIKO2_CMD=("${REPO_ROOT}/bin/gaiko2")
elif [[ -x "${REPO_ROOT}/gaiko2" ]]; then
    GAIKO2_CMD=("${REPO_ROOT}/gaiko2")
else
    GAIKO2_CMD=(go run ./cmd/gaiko2)
fi

run_gaiko2() {
    (
        cd "${REPO_ROOT}"
        "${GAIKO2_CMD[@]}" "$@"
    )
}

key_path="${GAIKO2_SECRET_DIR}/priv.gaiko2.key"
if [[ ! -f "${key_path}" ]]; then
    echo "[gaiko2-tdx] bootstrapping tee key into ${GAIKO2_SECRET_DIR}"
    run_gaiko2 bootstrap \
        --tee-type "${GAIKO2_TEE_TYPE}" \
        --secret-dir "${GAIKO2_SECRET_DIR}" \
        --config-dir "${GAIKO2_CONFIG_DIR}" \
        --tdxs-socket "${GAIKO2_TDXS_SOCKET}"
else
    echo "[gaiko2-tdx] using existing tee key ${key_path}"
fi

echo "[gaiko2-tdx] checking tee key"
run_gaiko2 check \
    --tee-type "${GAIKO2_TEE_TYPE}" \
    --secret-dir "${GAIKO2_SECRET_DIR}" \
    --tdxs-socket "${GAIKO2_TDXS_SOCKET}"

echo "[gaiko2-tdx] starting replay prover on :${GAIKO2_PORT}"
cd "${REPO_ROOT}"
exec "${GAIKO2_CMD[@]}" server
