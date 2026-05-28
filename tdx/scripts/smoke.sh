#!/usr/bin/env bash

set -euo pipefail

GAIKO2_HEALTH_URL=${GAIKO2_HEALTH_URL:-http://127.0.0.1:8080/healthz}
L2_RPC_URL=${L2_RPC_URL:-http://127.0.0.1:8545}

curl -fsS "${GAIKO2_HEALTH_URL}" >/dev/null
curl -fsS \
    -H "content-type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
    "${L2_RPC_URL}" >/dev/null

echo "ok"
