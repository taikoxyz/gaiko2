#!/usr/bin/env bash

set -euo pipefail

binary_path="${1:?missing enclave binary path}"
enclave_json="${2:?missing enclave json path}"
output_path="${3:?missing output path}"

extract_hex() {
    grep -Eo '[0-9a-fA-F]{64,}' | head -n1 | tr '[:upper:]' '[:lower:]'
}

unique_id=$(ego uniqueid "${binary_path}" | extract_hex)
signer_id=$(ego signerid "${binary_path}" | extract_hex)
product_id=$(sed -n 's/.*"productID"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${enclave_json}" | head -n1)
security_version=$(sed -n 's/.*"securityVersion"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${enclave_json}" | head -n1)

if [[ -z "${unique_id}" ]]; then
    echo "failed to extract unique id" >&2
    exit 1
fi
if [[ -z "${signer_id}" ]]; then
    echo "failed to extract signer id" >&2
    exit 1
fi
if [[ -z "${product_id}" ]]; then
    echo "failed to extract productID from ${enclave_json}" >&2
    exit 1
fi
if [[ -z "${security_version}" ]]; then
    echo "failed to extract securityVersion from ${enclave_json}" >&2
    exit 1
fi

cat >"${output_path}" <<EOF
{
  "unique_id": "${unique_id}",
  "signer_id": "${signer_id}",
  "product_id": ${product_id},
  "security_version": ${security_version}
}
EOF
