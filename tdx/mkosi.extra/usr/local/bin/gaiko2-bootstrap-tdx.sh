#!/usr/bin/env bash

set -euo pipefail

: "${GAIKO2_TEE_TYPE:=tdx}"
: "${GAIKO2_TDXS_SOCKET:=/var/tdxs.sock}"
: "${GAIKO2_CONFIG_DIR:=/persistent/gaiko2/config}"
: "${GAIKO2_SECRET_DIR:=/persistent/gaiko2/secrets}"

mkdir -p "${GAIKO2_CONFIG_DIR}" "${GAIKO2_SECRET_DIR}"

if [ ! -f "${GAIKO2_SECRET_DIR}/priv.gaiko2.key" ]; then
    /usr/bin/gaiko2 bootstrap \
        --tee-type "${GAIKO2_TEE_TYPE}" \
        --tdxs-socket "${GAIKO2_TDXS_SOCKET}" \
        --config-dir "${GAIKO2_CONFIG_DIR}" \
        --secret-dir "${GAIKO2_SECRET_DIR}" \
        >"${GAIKO2_CONFIG_DIR}/bootstrap.gaiko2.json"
fi

/usr/bin/gaiko2 check \
    --tee-type "${GAIKO2_TEE_TYPE}" \
    --tdxs-socket "${GAIKO2_TDXS_SOCKET}" \
    --secret-dir "${GAIKO2_SECRET_DIR}"
