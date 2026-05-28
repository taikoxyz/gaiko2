#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../.." && pwd)

RELEASE_TAG=${RELEASE_TAG:-dev}
OUT=${OUT:-"${REPO_ROOT}/tdx/manifests/${RELEASE_TAG}.json"}
STRICT=${STRICT:-0}

sha256_file() {
    local path="$1"
    if [ ! -f "${path}" ]; then
        if [ "${STRICT}" = "1" ]; then
            echo "missing required file: ${path}" >&2
            exit 1
        fi
        return 0
    fi
    sha256sum "${path}" | awk '{print $1}'
}

component_json() {
    local name="$1"
    local commit="$2"
    local path="$3"
    local sha
    sha=$(sha256_file "${path}")
    python3 - "$name" "$commit" "$path" "$sha" <<'PY'
import json
import sys

name, commit, path, sha = sys.argv[1:]
print(json.dumps({
    "name": name,
    "commit": commit or None,
    "binary": {
        "path": path,
        "sha256": sha or None,
    },
}, sort_keys=True))
PY
}

profile_files_json() {
    (
        cd "${REPO_ROOT}"
        find \
            tdx/mkosi.conf \
            tdx/mkosi.extra/etc/gaiko2 \
            tdx/mkosi.extra/etc/systemd/system \
            tdx/mkosi.extra/etc/tdxs \
            tdx/mkosi.extra/usr/local/bin \
            -type f -print
    ) | sort | while IFS= read -r rel; do
        local sha
        sha=$(sha256_file "${REPO_ROOT}/${rel}")
        python3 - "$rel" "$sha" <<'PY'
import json
import sys

path, sha = sys.argv[1:]
print(json.dumps({"path": path, "sha256": sha}, sort_keys=True))
PY
    done
}

GAIKO2_COMMIT=${GAIKO2_COMMIT:-$(cd "${REPO_ROOT}" && git rev-parse HEAD 2>/dev/null || true)}
TAIKO_GETH_COMMIT=${TAIKO_GETH_COMMIT:-}
TAIKO_CLIENT_COMMIT=${TAIKO_CLIENT_COMMIT:-}
TDXS_COMMIT=${TDXS_COMMIT:-}

GAIKO2_BIN=${GAIKO2_BIN:-/usr/bin/gaiko2}
TAIKO_GETH_BIN=${TAIKO_GETH_BIN:-/usr/bin/taiko-geth}
TAIKO_CLIENT_BIN=${TAIKO_CLIENT_BIN:-/usr/bin/taiko-client}
TDXS_BIN=${TDXS_BIN:-/usr/bin/tdxs}

if [ "${STRICT}" = "1" ]; then
    : "${TDX_IMAGE_ID:?TDX_IMAGE_ID is required when STRICT=1}"
    : "${TDX_MRTD:?TDX_MRTD is required when STRICT=1}"
fi

mkdir -p "$(dirname "${OUT}")"

components_tmp=$(mktemp)
files_tmp=$(mktemp)
trap 'rm -f "${components_tmp}" "${files_tmp}"' EXIT

{
    component_json gaiko2 "${GAIKO2_COMMIT}" "${GAIKO2_BIN}"
    component_json taiko-geth "${TAIKO_GETH_COMMIT}" "${TAIKO_GETH_BIN}"
    component_json taiko-client "${TAIKO_CLIENT_COMMIT}" "${TAIKO_CLIENT_BIN}"
    component_json tdxs "${TDXS_COMMIT}" "${TDXS_BIN}"
} >"${components_tmp}"

profile_files_json >"${files_tmp}"

python3 - "${OUT}" "${components_tmp}" "${files_tmp}" <<'PY'
import datetime as dt
import json
import os
import sys

out_path, components_path, files_path = sys.argv[1:]

with open(components_path, encoding="utf-8") as f:
    components = [json.loads(line) for line in f if line.strip()]
with open(files_path, encoding="utf-8") as f:
    files = [json.loads(line) for line in f if line.strip()]

manifest = {
    "schema": "tdx-gaiko2-image-manifest-v1",
    "release_tag": os.environ.get("RELEASE_TAG", "dev"),
    "generated_at": dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "provider": {
        "name": "tdx-gaiko2",
        "proof_type": "tdxgeth",
        "protocol": "raiko2-remote-prover",
    },
    "components": components,
    "runtime": {
        "gaiko2_http": os.environ.get("GAIKO2_HTTP", "0.0.0.0:8080"),
        "local_l2_rpc": os.environ.get("GAIKO2_L2_RPC_URL", "http://127.0.0.1:8545"),
        "tdxs_socket": os.environ.get("GAIKO2_TDXS_SOCKET", "/var/tdxs.sock"),
        "persistent_root": os.environ.get("PERSISTENT_ROOT", "/persistent"),
    },
    "measurement": {
        "tdx_image_id": os.environ.get("TDX_IMAGE_ID"),
        "mrtd": os.environ.get("TDX_MRTD"),
        "rtmr0": os.environ.get("TDX_RTMR0"),
        "rtmr1": os.environ.get("TDX_RTMR1"),
        "rtmr2": os.environ.get("TDX_RTMR2"),
        "rtmr3": os.environ.get("TDX_RTMR3"),
        "verifier": os.environ.get("TDX_VERIFIER"),
    },
    "statement_affecting_files": files,
}

with open(out_path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2, sort_keys=True)
    f.write("\n")
PY

echo "wrote ${OUT}"
