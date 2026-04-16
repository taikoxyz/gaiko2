#!/usr/bin/env bash

set -euo pipefail

: "${GAIKO2_REGISTERED_JSON:?missing GAIKO2_REGISTERED_JSON}"
: "${GAIKO2_FORK:?missing GAIKO2_FORK}"

if [[ -z "${GAIKO2_REGISTER_INSTANCE_ID:-}" ]]; then
    cat <<EOF
register hook example

Inputs:
  bootstrap: ${GAIKO2_BOOTSTRAP_JSON:-}
  attestation: ${GAIKO2_ATTESTATION_JSON:-}
  registered target: ${GAIKO2_REGISTERED_JSON}
  fork: ${GAIKO2_FORK}
  release: ${GAIKO2_RELEASE:-}

This example hook does not call a verifier. To use it as a manual fallback,
export GAIKO2_REGISTER_INSTANCE_ID=<instance-id> and rerun the hook.
EOF
    exit 1
fi

mkdir -p "$(dirname -- "${GAIKO2_REGISTERED_JSON}")"
cat >"${GAIKO2_REGISTERED_JSON}" <<EOF
{
  "${GAIKO2_FORK}": ${GAIKO2_REGISTER_INSTANCE_ID}
}
EOF

echo "wrote ${GAIKO2_REGISTERED_JSON}"
