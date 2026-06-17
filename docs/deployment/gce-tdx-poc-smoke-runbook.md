# GCE TDX POC Smoke Runbook

Date: 2026-06-17

## Purpose

This is a smoke runbook for the historical node-based `tdxgeth` POC in PR #6.
It is not a production trust procedure.

The goal is to check that a GCE Confidential VM can run the TDX image profile
and expose enough TDX devices and services for the POC flow.

## Confirm VM Shape

Run on the VM:

```bash
hostnamectl
systemd-detect-virt
sudo dmidecode -s system-manufacturer
sudo dmidecode -s system-product-name
curl -fsS -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/machine-type || true
curl -fsS -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/zone || true
```

Expected signals:

- `systemd-detect-virt` reports `google`.
- Manufacturer/model identify Google Compute Engine.
- Machine type is a TDX-capable confidential VM type.

## Confirm TDX Devices

Run:

```bash
ls -l /dev/tdx_guest /dev/tpm* 2>/dev/null || true
sudo dmesg | grep -iE 'tdx|confidential|tpm|vtpm' | tail -120
```

Expected:

- `/dev/tdx_guest` exists.
- `/dev/tpm0` or `/dev/tpmrm0` exists when vTPM-backed storage or attestation
  helpers are expected.
- Kernel logs show TDX/confidential guest support.

## Build The POC Image

From this repository branch:

```bash
cd tdx
./scripts/test-image-profile.sh
./scripts/build-image.sh
./scripts/export-manifest.sh
```

The exported manifest is the operator-facing record of the measured image inputs.
It must be kept with any registration or smoke-test result.

## Bootstrap The Provider

Inside the VM or image test environment:

```bash
export GAIKO2_PROVING_MODE=tdxgeth
export GAIKO2_TEE_TYPE=tdx
export GAIKO2_TDXS_SOCKET=/var/tdxs.sock
export GAIKO2_CONFIG_DIR=/persistent/gaiko2/config
export GAIKO2_SECRET_DIR=/persistent/gaiko2/secrets
export GAIKO2_FORK=shasta

gaiko2 bootstrap
gaiko2 check
```

The bootstrap output should contain the public key, instance address, and TDX
attestation data needed by registration tooling.

## Start Services

The expected service order is documented in `docs/deployment/tdx-gaiko2.md`.
For a systemd-based image, check:

```bash
systemctl status tdxs.socket tdxs.service
systemctl status taiko-geth.service taiko-client.service gaiko2-tdxgeth.service
curl -fsS http://127.0.0.1:8080/healthz
```

For the POC statement, taiko-geth and taiko-client must be local to the measured
VM. Do not point `GAIKO2_L2_RPC_URL` at an arbitrary external RPC when evaluating
the node-based POC boundary.

## Smoke Test Through Raiko2

From an external raiko2 host, configure the explicit `tdxgeth` lane to call the
provider URL exposed by the VM, then run:

- remote prover proposal conformance;
- remote prover aggregate conformance;
- one real Shasta proposal or direct aggregate request.

The result is only a POC smoke pass. Production use still requires a hardened
image identity, registration policy, L1 RPC freshness policy, storage rollback
policy, and no-debug/no-SSH image rules.
