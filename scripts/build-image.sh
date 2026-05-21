#!/usr/bin/env bash
set -euo pipefail

mode="${1:-native}"
tag="${2:-latest}"
secret_args=()

case "$mode" in
native)
    image_name="gaiko2-native"
    target_dockerfile="docker/Dockerfile.native"
    ;;
tee|ego)
    image_name="gaiko2-tee"
    target_dockerfile="docker/Dockerfile.tee"
    signing_key="${GAIKO2_EGO_SIGNING_KEY:-}"
    if [[ -z "${signing_key}" ]]; then
        echo "GAIKO2_EGO_SIGNING_KEY is required when building tee images" >&2
        exit 1
    fi
    if [[ ! -f "${signing_key}" ]]; then
        echo "GAIKO2_EGO_SIGNING_KEY does not exist: ${signing_key}" >&2
        exit 1
    fi
    secret_args=(--secret "id=ego_signing_key,src=${signing_key}")
    ;;
*)
    echo "unsupported image mode: $mode (expected native or tee)" >&2
    exit 1
    ;;
esac

docker buildx build \
    -f "$target_dockerfile" \
    --load \
    --platform linux/amd64 \
    -t "${image_name}:${tag}" \
    "${secret_args[@]}" \
    --progress=plain \
    .
