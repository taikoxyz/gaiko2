#!/usr/bin/env bash

exec 2>&1
set -euo pipefail

GAIKO2_BIN=${GAIKO2_BIN:-/opt/gaiko2/bin/gaiko2}
GAIKO2_TEE_TYPE=${GAIKO2_TEE_TYPE:-ego}
GAIKO2_CONFIG_DIR=${GAIKO2_CONFIG_DIR:-/var/lib/gaiko2/config}
GAIKO2_SECRET_DIR=${GAIKO2_SECRET_DIR:-/var/lib/gaiko2/secrets}
GAIKO2_ATTESTATION_PATH=${GAIKO2_ATTESTATION_PATH:-/opt/gaiko2/etc/attestation.json}
GAIKO2_ALLOW_INSECURE_PCCS=${GAIKO2_ALLOW_INSECURE_PCCS:-0}
SGX_QCNL_CONF=${SGX_QCNL_CONF:-/etc/sgx_default_qcnl.conf}

mkdir -p "$GAIKO2_CONFIG_DIR" "$GAIKO2_SECRET_DIR"

if [[ -f "${GAIKO2_ATTESTATION_PATH}" ]]; then
    cp "${GAIKO2_ATTESTATION_PATH}" "${GAIKO2_CONFIG_DIR}/attestation.gaiko2.json"
fi

if [[ -f "${SGX_QCNL_CONF}" ]]; then
    MY_PCCS_HOST=${PCCS_HOST:-pccs:8081}
    sed -i "s#https://localhost:8081#https://${MY_PCCS_HOST}#g" "${SGX_QCNL_CONF}" || true
    if [[ "${GAIKO2_ALLOW_INSECURE_PCCS}" == "1" ]]; then
        sed -i 's/,"use_secure_cert": true/,"use_secure_cert": false/' "${SGX_QCNL_CONF}" || true
    fi
fi

if [[ -x /restart_aesm.sh ]]; then
    /restart_aesm.sh
fi

if [[ $# -eq 0 ]]; then
    exec "$GAIKO2_BIN" server
fi

case "$1" in
--init|init)
    shift
    exec "$GAIKO2_BIN" bootstrap \
        --tee-type "$GAIKO2_TEE_TYPE" \
        --secret-dir "$GAIKO2_SECRET_DIR" \
        --config-dir "$GAIKO2_CONFIG_DIR" \
        "$@"
    ;;
--check|check)
    shift
    exec "$GAIKO2_BIN" check \
        --tee-type "$GAIKO2_TEE_TYPE" \
        --secret-dir "$GAIKO2_SECRET_DIR" \
        "$@"
    ;;
server|serve|s)
    exec "$GAIKO2_BIN" "$@"
    ;;
*)
    exec "$GAIKO2_BIN" "$@"
    ;;
esac
