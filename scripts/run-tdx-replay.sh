#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)

export GAIKO2_PROVING_MODE="${GAIKO2_PROVING_MODE:-tee}"
export GAIKO2_TEE_TYPE="${GAIKO2_TEE_TYPE:-tdx}"
export GAIKO2_CONFIG_DIR="${GAIKO2_CONFIG_DIR:-${HOME}/.config/gaiko2/tdx/config}"
export GAIKO2_SECRET_DIR="${GAIKO2_SECRET_DIR:-${HOME}/.config/gaiko2/tdx/secrets}"
export GAIKO2_PORT="${GAIKO2_PORT:-8080}"

discover_tdxs_socket() {
    if [[ -n "${GAIKO2_TDXS_SOCKET:-}" ]]; then
        printf '%s\n' "${GAIKO2_TDXS_SOCKET}"
        return 0
    fi
    if [[ -S /var/tdxs.sock ]]; then
        printf '%s\n' "/var/tdxs.sock"
        return 0
    fi
    if command -v ss >/dev/null 2>&1; then
        local candidate
        candidate=$(
            ss -xlpnH 2>/dev/null | awk '{
                for (i = 1; i <= NF; i++) {
                    if ($i ~ /(^|\/)tdxs\.sock$/) {
                        print $i
                        exit
                    }
                }
            }'
        )
        if [[ -n "${candidate}" && -S "${candidate}" ]]; then
            printf '%s\n' "${candidate}"
            return 0
        fi
    fi

    local search_dirs
    search_dirs="${GAIKO2_TDXS_SOCKET_SEARCH_DIRS:-/run /var /tmp /mnt}"
    local root candidate
    for root in ${search_dirs}; do
        [[ -d "${root}" ]] || continue
        candidate=$(find "${root}" -type s -name tdxs.sock -print -quit 2>/dev/null || true)
        if [[ -n "${candidate}" && -S "${candidate}" ]]; then
            printf '%s\n' "${candidate}"
            return 0
        fi
    done
    printf '%s\n' "/var/tdxs.sock"
}

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
export GAIKO2_TDXS_SOCKET
GAIKO2_TDXS_SOCKET=$(discover_tdxs_socket)
if [[ ! -S "${GAIKO2_TDXS_SOCKET}" ]]; then
    echo "error: TDXS socket not found: ${GAIKO2_TDXS_SOCKET}" >&2
    echo "hint: set GAIKO2_TDXS_SOCKET to the socket shown by: ss -xlpn | grep tdxs.sock" >&2
    exit 1
fi
echo "[gaiko2-tdx] using TDXS socket ${GAIKO2_TDXS_SOCKET}"

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
