#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)
RUN_SCRIPT="${SCRIPT_DIR}/run-tdx-replay.sh"

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

socket_dir="${tmpdir}/runtime/local-run"
socket_path="${socket_dir}/tdxs.sock"
mkdir -p "${socket_dir}"

python3 - "${socket_path}" <<'PY' &
import signal
import socket
import sys
import time

path = sys.argv[1]
sock = socket.socket(socket.AF_UNIX)
sock.bind(path)
sock.listen(1)
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
while True:
    time.sleep(1)
PY
socket_pid=$!
trap 'kill "${socket_pid}" 2>/dev/null || true; rm -rf "${tmpdir}"' EXIT

for _ in $(seq 1 50); do
    [[ -S "${socket_path}" ]] && break
    sleep 0.1
done
[[ -S "${socket_path}" ]] || {
    echo "failed to create unix socket fixture" >&2
    exit 1
}

stub="${tmpdir}/gaiko2-stub.sh"
stub_out="${tmpdir}/stub.out"
cat >"${stub}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${STUB_OUT}"
if [[ "${1:-}" == "server" ]]; then
    exit 0
fi
EOF
chmod +x "${stub}"

GAIKO2_BIN="${stub}" \
STUB_OUT="${stub_out}" \
GAIKO2_INSTANCE_ID=1 \
GAIKO2_CONFIG_DIR="${tmpdir}/config" \
GAIKO2_SECRET_DIR="${tmpdir}/secrets" \
GAIKO2_TDXS_SOCKET_SEARCH_DIRS="${tmpdir}" \
bash "${RUN_SCRIPT}" >"${tmpdir}/run.out"

grep -F "using TDXS socket ${socket_path}" "${tmpdir}/run.out" >/dev/null
grep -F -- "--tdxs-socket ${socket_path}" "${stub_out}" >/dev/null
grep -F "server" "${stub_out}" >/dev/null

echo "ok"
