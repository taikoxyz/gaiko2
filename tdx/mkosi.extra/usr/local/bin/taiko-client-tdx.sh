#!/usr/bin/env bash

set -euo pipefail

: "${L1_ENDPOINT_WS:?L1_ENDPOINT_WS is required}"
: "${L1_BEACON_HTTP:?L1_BEACON_HTTP is required}"
: "${SHASTA_INBOX_ADDRESS:?SHASTA_INBOX_ADDRESS is required}"
: "${TAIKO_ANCHOR_ADDRESS:?TAIKO_ANCHOR_ADDRESS is required}"
: "${TAIKO_INTERNAL_SHASTA_TIME:?TAIKO_INTERNAL_SHASTA_TIME is required}"

TAIKO_GETH_DATADIR=${TAIKO_GETH_DATADIR:-/persistent/taiko-geth}
TAIKO_CLIENT_DATA_DIR=${TAIKO_CLIENT_DATA_DIR:-/persistent/taiko-client}
VERBOSITY=${VERBOSITY:-3}

mkdir -p "${TAIKO_CLIENT_DATA_DIR}"

args=(
    driver
    --l1.ws "${L1_ENDPOINT_WS}"
    --l2.ws "ws://127.0.0.1:8546"
    --l1.beacon "${L1_BEACON_HTTP}"
    --l2.auth "http://127.0.0.1:8551"
    --shastaInbox "${SHASTA_INBOX_ADDRESS}"
    --shasta.time "${TAIKO_INTERNAL_SHASTA_TIME}"
    --taikoAnchor "${TAIKO_ANCHOR_ADDRESS}"
    --verbosity "${VERBOSITY}"
    --jwtSecret "${TAIKO_GETH_DATADIR}/geth/jwtsecret"
)

if [ -n "${TAIKO_INBOX_ADDRESS:-}" ]; then
    args+=(--pacayaInbox "${TAIKO_INBOX_ADDRESS}")
fi

if [ -n "${PRECONFIRMATION_WHITELIST:-}" ]; then
    args+=(--preconfirmation.whitelist "${PRECONFIRMATION_WHITELIST}")
fi

if [ -n "${BLOB_SERVER_URL:-}" ]; then
    args+=(--blob.server "${BLOB_SERVER_URL}")
fi

if [ "${DISABLE_P2P_SYNC:-false}" = "false" ]; then
    : "${P2P_SYNC_URL:?P2P_SYNC_URL is required when p2p sync is enabled}"
    args+=(--p2p.sync --p2p.checkPointSyncUrl "${P2P_SYNC_URL}")
fi

if [ "${ENABLE_PRECONFS_P2P:-false}" = "true" ]; then
    : "${P2P_BOOTNODES:?P2P_BOOTNODES is required when preconf p2p is enabled}"
    : "${P2P_PRIV_PATH:?P2P_PRIV_PATH is required when preconf p2p is enabled}"
    args+=(
        --p2p.peerstore.path "${TAIKO_CLIENT_DATA_DIR}/peerstore"
        --p2p.discovery.path "${TAIKO_CLIENT_DATA_DIR}/discv5"
        --preconfirmation.serverPort 9871
        --p2p.listen.ip "0.0.0.0"
        --p2p.useragent taiko
        --p2p.bootnodes "${P2P_BOOTNODES}"
        --p2p.priv.path "${P2P_PRIV_PATH}"
    )
    if [ -n "${PUBLIC_IP:-}" ]; then
        args+=(
            --p2p.advertise.ip "${PUBLIC_IP}"
            --p2p.advertise.udp "${P2P_UDP_PORT:-30303}"
            --p2p.listen.udp "${P2P_UDP_PORT:-30303}"
            --p2p.advertise.tcp "${P2P_TCP_PORT:-4001}"
            --p2p.listen.tcp "${P2P_TCP_PORT:-4001}"
        )
    else
        args+=(--p2p.nat)
    fi
fi

exec /usr/bin/taiko-client "${args[@]}"
