#!/usr/bin/env bash
set -euo pipefail

mode="${1:-native}"
tag="${2:-latest}"
build_args=()
tmp_files=()
fetched_gcp_enclave_key=""

cleanup() {
    if ((${#tmp_files[@]} > 0)); then
        rm -f "${tmp_files[@]}"
    fi
}
trap cleanup EXIT

env_value() {
    local name="$1"
    local value="${!name:-}"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "${value}"
}

fetch_gcp_enclave_key() {
    local secret="$1"
    local tmp_key
    tmp_key=$(mktemp)
    tmp_files+=("${tmp_key}")
    chmod 600 "${tmp_key}"

    local version
    version=$(env_value GCP_ENCLAVE_KEY_VERSION)
    if [[ -z "${version}" ]]; then
        version="latest"
    fi

    local gcloud_args=(
        secrets
        versions
        access
        "${version}"
        --secret
        "${secret}"
    )
    local project
    project=$(env_value GCP_ENCLAVE_KEY_PROJECT)
    if [[ -n "${project}" ]]; then
        gcloud_args+=(--project "${project}")
    fi

    gcloud "${gcloud_args[@]}" >"${tmp_key}"
    if [[ ! -s "${tmp_key}" ]]; then
        echo "GCP Secret Manager returned an empty enclave signing key for ${secret}" >&2
        exit 1
    fi

    fetched_gcp_enclave_key="${tmp_key}"
}

case "$mode" in
native)
    image_name="gaiko2-native"
    target_dockerfile="docker/Dockerfile.native"
    ;;
tee|ego)
    image_name="gaiko2-tee"
    target_dockerfile="docker/Dockerfile.tee"

    gcp_secret=$(env_value GCP_ENCLAVE_KEY_SECRET)
    public_sha256=$(env_value ENCLAVE_KEY_PUBLIC_SHA256)
    if [[ -n "${gcp_secret}" ]]; then
        fetch_gcp_enclave_key "${gcp_secret}"
        build_args+=(--secret "id=enclave_key,src=${fetched_gcp_enclave_key}")
        if [[ -n "${public_sha256}" ]]; then
            build_args+=(--build-arg "ENCLAVE_KEY_PUBLIC_SHA256=${public_sha256}")
        fi
    elif [[ -n "${public_sha256}" ]]; then
        echo "ENCLAVE_KEY_PUBLIC_SHA256 requires GCP_ENCLAVE_KEY_SECRET; unset it for disposable local builds" >&2
        exit 1
    fi
    ;;
*)
    echo "unsupported image mode: $mode (expected native or tee)" >&2
    exit 1
    ;;
esac

DOCKER_BUILDKIT=1 docker buildx build . \
    -f "$target_dockerfile" \
    --load \
    --platform linux/amd64 \
    "${build_args[@]}" \
    -t "${image_name}:${tag}" \
    --progress=plain
