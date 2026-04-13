#!/usr/bin/env bash
set -euo pipefail

mode="${1:-native}"
tag="${2:-latest}"

case "$mode" in
native)
    image_name="gaiko2-native"
    target_dockerfile="docker/Dockerfile.native"
    ;;
tee|ego)
    image_name="gaiko2-tee"
    target_dockerfile="docker/Dockerfile.tee"
    ;;
*)
    echo "unsupported image mode: $mode (expected native or tee)" >&2
    exit 1
    ;;
esac

docker buildx build . \
    -f "$target_dockerfile" \
    --load \
    --platform linux/amd64 \
    -t "${image_name}:${tag}" \
    --progress=plain
