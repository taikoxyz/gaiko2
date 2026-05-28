#!/usr/bin/env bash

set -euo pipefail

: "${CHAIN_ID:?CHAIN_ID is required}"
: "${BOOT_NODES:?BOOT_NODES is required}"

TAIKO_GETH_DATADIR=${TAIKO_GETH_DATADIR:-/persistent/taiko-geth}
GETH_P2P_PORT=${GETH_P2P_PORT:-30306}
GETH_MAXPEERS=${GETH_MAXPEERS:-50}
GETH_MAXPENDPEERS=${GETH_MAXPENDPEERS:-0}

mkdir -p "${TAIKO_GETH_DATADIR}"

exec /usr/bin/taiko-geth \
    --taiko \
    --networkid "${CHAIN_ID}" \
    --gcmode archive \
    --syncmode full \
    --datadir "${TAIKO_GETH_DATADIR}" \
    --metrics \
    --metrics.expensive \
    --metrics.addr "127.0.0.1" \
    --bootnodes "${BOOT_NODES}" \
    --authrpc.addr "127.0.0.1" \
    --authrpc.vhosts "localhost,127.0.0.1" \
    --http \
    --http.api "debug,eth,net,web3,txpool,taiko" \
    --http.addr "127.0.0.1" \
    --http.vhosts "localhost,127.0.0.1" \
    --ws \
    --ws.api "debug,eth,net,web3,txpool,taiko" \
    --ws.addr "127.0.0.1" \
    --ws.origins "http://localhost,http://127.0.0.1" \
    --gpo.ignoreprice "25000000" \
    --port "${GETH_P2P_PORT}" \
    --discovery.port "${GETH_P2P_PORT}" \
    --maxpeers "${GETH_MAXPEERS}" \
    --maxpendpeers "${GETH_MAXPENDPEERS}"
