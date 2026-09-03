#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)
DEPLOY_SCRIPT="${SCRIPT_DIR}/deploy-tee.sh"

if [[ ! -f "${DEPLOY_SCRIPT}" ]]; then
    echo "missing deploy script: ${DEPLOY_SCRIPT}" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

export GAIKO2_TEST_SOURCE_ONLY=1
source "${DEPLOY_SCRIPT}"

DOCKER_LOG="${tmpdir}/docker.log"

docker() {
    printf '%s\n' "$*" >>"${DOCKER_LOG}"
    if [[ "${1:-}" == "compose" && "${2:-}" == "ps" ]]; then
        printf 'NAME STATUS\n'
    fi
}

assert_file_exists() {
    local path="$1"
    if [[ ! -f "${path}" ]]; then
        echo "expected file to exist: ${path}" >&2
        exit 1
    fi
}

assert_dir_exists() {
    local path="$1"
    if [[ ! -d "${path}" ]]; then
        echo "expected directory to exist: ${path}" >&2
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

test_status_reports_missing_bootstrap() {
    local output="${tmpdir}/status.out"
    main --fork unzen --release v1.0.0 --deploy-root "${tmpdir}/deploy" status >"${output}"
    assert_contains "${output}" "bootstrap: missing"
    assert_contains "${output}" "attestation: missing"
    assert_contains "${output}" "registered: missing"
}

test_init_creates_release_state_and_uses_release_project() {
    local output="${tmpdir}/init.out"
    main \
        --fork unzen \
        --release v1.0.0-rc.1 \
        --deploy-root "${tmpdir}/deploy" \
        --tee-image ghcr.io/taikoxyz/gaiko2-tee:v1 \
        --pccs-host pccs:8081 \
        --port 38080 \
        init >"${output}"

    local release_dir="${tmpdir}/deploy/unzen/v1.0.0-rc.1"
    assert_dir_exists "${release_dir}/config"
    assert_dir_exists "${release_dir}/secrets"
    assert_file_exists "${release_dir}/.env"
    assert_contains "${release_dir}/.env" "GAIKO2_TEE_IMAGE=ghcr.io/taikoxyz/gaiko2-tee:v1"
    assert_contains "${release_dir}/.env" "GAIKO2_FORK=unzen"
    assert_contains "${release_dir}/.env" "PCCS_HOST=pccs:8081"
    assert_contains "${release_dir}/.env" "GAIKO2_TEE_PORT=38080"
    assert_contains "${release_dir}/.env" "GAIKO2_CONFIG_DIR_HOST=${release_dir}/config"
    assert_contains "${release_dir}/.env" "GAIKO2_SECRET_DIR_HOST=${release_dir}/secrets"
    assert_contains "${DOCKER_LOG}" "compose --project-name gaiko2-unzen-v1-0-0-rc-1 --env-file ${release_dir}/.env -f ${REPO_ROOT}/compose.yaml --profile tee-init run --rm gaiko2-tee-init"
}

test_status_reports_generated_release_env() {
    local output="${tmpdir}/status-after-init.out"
    main --fork unzen --release v1.0.0-rc.1 --deploy-root "${tmpdir}/deploy" status >"${output}"
    assert_contains "${output}" "env: present"
    assert_contains "${output}" "bootstrap: missing"
    assert_contains "${output}" "attestation: missing"
    assert_contains "${output}" "compose project: gaiko2-unzen-v1-0-0-rc-1"
}

test_up_persists_port_override_for_existing_release() {
    local release_dir="${tmpdir}/deploy/unzen/v1.0.0-rc.1"
    : >"${release_dir}/secrets/priv.gaiko2.key"
    printf '{\n  "unzen": 1234\n}\n' >"${release_dir}/config/registered.gaiko2.json"

    main \
        --fork unzen \
        --release v1.0.0-rc.1 \
        --deploy-root "${tmpdir}/deploy" \
        --port 39090 \
        up >/dev/null

    assert_contains "${release_dir}/.env" "GAIKO2_TEE_PORT=39090"
    assert_contains "${DOCKER_LOG}" "compose --project-name gaiko2-unzen-v1-0-0-rc-1 --env-file ${release_dir}/.env -f ${REPO_ROOT}/compose.yaml --profile tee up -d --wait gaiko2-tee"
}

test_down_targets_only_the_release_service() {
    main --fork unzen --release v1.0.0-rc.1 --deploy-root "${tmpdir}/deploy" down >/dev/null
    assert_contains "${DOCKER_LOG}" "compose --project-name gaiko2-unzen-v1-0-0-rc-1 --env-file ${tmpdir}/deploy/unzen/v1.0.0-rc.1/.env -f ${REPO_ROOT}/compose.yaml stop gaiko2-tee"
    assert_contains "${DOCKER_LOG}" "compose --project-name gaiko2-unzen-v1-0-0-rc-1 --env-file ${tmpdir}/deploy/unzen/v1.0.0-rc.1/.env -f ${REPO_ROOT}/compose.yaml rm -sf gaiko2-tee"
}

test_register_hook_receives_attestation_path() {
    local release_dir="${tmpdir}/deploy/unzen/v1.0.0-rc.1"
    local hook="${tmpdir}/hook.sh"
    local hook_out="${tmpdir}/hook.out"

    cat >"${hook}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'attestation=%s\n' "${GAIKO2_ATTESTATION_JSON}" >"${HOOK_OUT}"
cat >"${GAIKO2_REGISTERED_JSON}" <<JSON
{
  "${GAIKO2_FORK}": 4567
}
JSON
EOF
    chmod +x "${hook}"

    printf '{\n  "quote": "0x01"\n}\n' >"${release_dir}/config/bootstrap.gaiko2.json"
    printf '{\n  "unique_id": "abc",\n  "signer_id": "def",\n  "product_id": 1,\n  "security_version": 1\n}\n' >"${release_dir}/config/attestation.gaiko2.json"

    HOOK_OUT="${hook_out}" main \
        --fork unzen \
        --release v1.0.0-rc.1 \
        --deploy-root "${tmpdir}/deploy" \
        --register-hook "${hook}" \
        register >/dev/null

    assert_contains "${hook_out}" "attestation=${release_dir}/config/attestation.gaiko2.json"
    assert_file_exists "${release_dir}/config/registered.gaiko2.json"
}

# Deploy directories use FORK and RELEASE verbatim, but compose_project_name
# slugifies both. Names differing only in case or punctuation therefore own
# separate deploy trees while sharing one compose project, so up/down under one
# spelling acts on the other's containers. Pin that so a slugify change cannot
# silently re-home running deployments.
test_project_name_slugifies_both_identifiers() {
    local saved_fork="${FORK}" saved_release="${RELEASE}"
    local fork release project
    local -a seen_dirs=()

    for fork in unzen Unzen UNZEN; do
        for release in v1.0.0 v1-0-0 v1_0_0; do
            FORK="${fork}"
            RELEASE="${release}"
            project=$(compose_project_name)
            if [[ "${project}" != "gaiko2-unzen-v1-0-0" ]]; then
                echo "expected gaiko2-unzen-v1-0-0 for --fork ${fork} --release ${release}, got ${project}" >&2
                exit 1
            fi
            seen_dirs+=("$(release_dir)")
        done
    done

    local unique_dirs
    unique_dirs=$(printf '%s\n' "${seen_dirs[@]}" | sort -u | wc -l | tr -d ' ')
    if [[ "${unique_dirs}" != "9" ]]; then
        echo "expected 9 distinct deploy dirs colliding on one project, got ${unique_dirs}" >&2
        exit 1
    fi

    FORK="${saved_fork}"
    RELEASE="${saved_release}"
}

test_status_reports_missing_bootstrap
test_init_creates_release_state_and_uses_release_project
test_status_reports_generated_release_env
test_up_persists_port_override_for_existing_release
test_down_targets_only_the_release_service
test_register_hook_receives_attestation_path
test_project_name_slugifies_both_identifiers

echo "ok"
