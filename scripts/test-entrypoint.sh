#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)
ENTRYPOINT="${REPO_ROOT}/docker/entrypoint.sh"

if [[ ! -f "${ENTRYPOINT}" ]]; then
    echo "missing entrypoint: ${ENTRYPOINT}" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

assert_file_exists() {
    local path="$1"
    if [[ ! -f "${path}" ]]; then
        echo "expected file to exist: ${path}" >&2
        exit 1
    fi
}

assert_contains() {
    local path="$1"
    local pattern="$2"
    if ! grep -Fq -- "${pattern}" "${path}"; then
        echo "expected ${path} to contain: ${pattern}" >&2
        echo "--- ${path} ---" >&2
        cat "${path}" >&2
        exit 1
    fi
}

test_init_copies_attestation_metadata_and_dispatches_bootstrap() {
    local stub="${tmpdir}/gaiko2-stub.sh"
    local stub_out="${tmpdir}/stub.out"
    local config_dir="${tmpdir}/config"
    local secret_dir="${tmpdir}/secrets"
    local attestation_src="${tmpdir}/attestation.json"
    local qcnl_conf="${tmpdir}/sgx_default_qcnl.conf"
    local attestation_dst="${config_dir}/attestation.gaiko2.json"

    mkdir -p "${config_dir}" "${secret_dir}"
    cat >"${attestation_src}" <<'EOF'
{
  "unique_id": "abc",
  "signer_id": "def",
  "product_id": 1,
  "security_version": 1
}
EOF
    cat >"${qcnl_conf}" <<'EOF'
PCCS_URL=https://localhost:8081/sgx/certification/v4/
EOF
    cat >"${stub}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"${STUB_OUT}"
EOF
    chmod +x "${stub}"

    GAIKO2_BIN="${stub}" \
        STUB_OUT="${stub_out}" \
        GAIKO2_TEE_TYPE=ego \
        GAIKO2_CONFIG_DIR="${config_dir}" \
        GAIKO2_SECRET_DIR="${secret_dir}" \
        GAIKO2_ATTESTATION_PATH="${attestation_src}" \
        SGX_QCNL_CONF="${qcnl_conf}" \
        PCCS_HOST="pccs:8081" \
        bash "${ENTRYPOINT}" init

    assert_file_exists "${attestation_dst}"
    assert_contains "${attestation_dst}" "\"unique_id\": \"abc\""
    assert_contains "${stub_out}" "bootstrap --tee-type ego --secret-dir ${secret_dir} --config-dir ${config_dir} --tdxs-socket /var/tdxs.sock"
    assert_contains "${qcnl_conf}" "https://pccs:8081/sgx/certification/v4/"
}

test_init_copies_attestation_metadata_and_dispatches_bootstrap

echo "ok"
