#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
BUILD_SCRIPT="${SCRIPT_DIR}/build-image.sh"

if [[ ! -f "${BUILD_SCRIPT}" ]]; then
    echo "missing build script: ${BUILD_SCRIPT}" >&2
    exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "${tmpdir}"' EXIT

fakebin="${tmpdir}/bin"
mkdir -p "${fakebin}"

DOCKER_LOG="${tmpdir}/docker.log"
GCLOUD_LOG="${tmpdir}/gcloud.log"

cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${DOCKER_LOG}"
EOF

cat >"${fakebin}/gcloud" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${GCLOUD_LOG}"
printf '%s\n' 'FAKE-PEM'
EOF

chmod +x "${fakebin}/docker" "${fakebin}/gcloud"

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

assert_not_contains() {
    local path="$1"
    local pattern="$2"
    if [[ -f "${path}" ]] && grep -Fq -- "${pattern}" "${path}"; then
        echo "expected ${path} to not contain: ${pattern}" >&2
        echo "--- ${path} ---" >&2
        cat "${path}" >&2
        exit 1
    fi
}

secret_src_from_docker_log() {
    sed -n 's/.*--secret id=enclave_key,src=\([^ ]*\).*/\1/p' "${DOCKER_LOG}" | tail -n1
}

test_tee_build_fetches_gcp_secret_and_mounts_it() {
    : >"${DOCKER_LOG}"
    : >"${GCLOUD_LOG}"

    PATH="${fakebin}:${PATH}" \
        DOCKER_LOG="${DOCKER_LOG}" \
        GCLOUD_LOG="${GCLOUD_LOG}" \
        GCP_ENCLAVE_KEY_SECRET="gaiko2-enclave-key" \
        GCP_ENCLAVE_KEY_VERSION="7" \
        GCP_ENCLAVE_KEY_PROJECT="taiko-project" \
        ENCLAVE_KEY_PUBLIC_SHA256="abc123" \
        bash "${BUILD_SCRIPT}" tee test-tag

    assert_contains "${GCLOUD_LOG}" "secrets versions access 7 --secret gaiko2-enclave-key --project taiko-project"
    assert_contains "${DOCKER_LOG}" "buildx build . -f docker/Dockerfile.tee"
    assert_contains "${DOCKER_LOG}" "--secret id=enclave_key,src="
    assert_contains "${DOCKER_LOG}" "--build-arg ENCLAVE_KEY_PUBLIC_SHA256=abc123"
    assert_contains "${DOCKER_LOG}" "-t gaiko2-tee:test-tag"

    local secret_src
    secret_src=$(secret_src_from_docker_log)
    if [[ -z "${secret_src}" ]]; then
        echo "failed to parse docker secret src" >&2
        cat "${DOCKER_LOG}" >&2
        exit 1
    fi
    if [[ -e "${secret_src}" ]]; then
        echo "expected temporary secret file to be removed: ${secret_src}" >&2
        exit 1
    fi
}

test_native_build_ignores_gcp_secret_env() {
    : >"${DOCKER_LOG}"
    : >"${GCLOUD_LOG}"

    PATH="${fakebin}:${PATH}" \
        DOCKER_LOG="${DOCKER_LOG}" \
        GCLOUD_LOG="${GCLOUD_LOG}" \
        GCP_ENCLAVE_KEY_SECRET="gaiko2-enclave-key" \
        bash "${BUILD_SCRIPT}" native test-tag

    assert_contains "${DOCKER_LOG}" "buildx build . -f docker/Dockerfile.native"
    assert_contains "${DOCKER_LOG}" "-t gaiko2-native:test-tag"
    assert_not_contains "${DOCKER_LOG}" "--secret id=enclave_key"
    if [[ -s "${GCLOUD_LOG}" ]]; then
        echo "native build should not fetch the enclave key secret" >&2
        cat "${GCLOUD_LOG}" >&2
        exit 1
    fi
}

test_tee_build_without_secret_uses_dockerfile_fallback() {
    : >"${DOCKER_LOG}"
    : >"${GCLOUD_LOG}"

    PATH="${fakebin}:${PATH}" \
        DOCKER_LOG="${DOCKER_LOG}" \
        GCLOUD_LOG="${GCLOUD_LOG}" \
        bash "${BUILD_SCRIPT}" tee local

    assert_contains "${DOCKER_LOG}" "buildx build . -f docker/Dockerfile.tee"
    assert_not_contains "${DOCKER_LOG}" "--secret id=enclave_key"
    if [[ -s "${GCLOUD_LOG}" ]]; then
        echo "tee build without GCP_ENCLAVE_KEY_SECRET should not call gcloud" >&2
        cat "${GCLOUD_LOG}" >&2
        exit 1
    fi
}

test_tee_build_fetches_gcp_secret_and_mounts_it
test_native_build_ignores_gcp_secret_env
test_tee_build_without_secret_uses_dockerfile_fallback

echo "ok"
