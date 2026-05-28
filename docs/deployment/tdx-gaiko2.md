# TDX-Gaiko2 Deployment Design

## Purpose

`tdx-gaiko2` is the TDX-local taiko-geth remote prover mode for `gaiko2`.

It does not prove that TDX executed the raiko2 guest. It proves that a registered
TDX instance, running an accepted measured VM image, checked the same Shasta
commitment against the taiko-geth node inside that same VM and signed the canonical
input hash.

## Runtime Shape

The trusted VM image should bake in:

- `gaiko2` built from this repository
- `taiko-geth`
- `taiko-client`
- `tdxs`
- systemd units and startup ordering
- statement-affecting config templates

Do not use a runtime model that pulls mutable containers or host-mounted scripts
inside the measured VM. Floating tags, `pull_policy: always`, and operator-provided
replacement binaries defeat the image identity boundary.

## Services

The expected service graph is:

```text
runtime-init.service
  -> tdxs.socket
  -> tdxs.service
  -> taiko-geth.service
  -> taiko-client.service
  -> gaiko2-tdxgeth.service
```

`gaiko2-tdxgeth.service` should run with:

```ini
Environment=GAIKO2_PROVING_MODE=tdxgeth
Environment=GAIKO2_TEE_TYPE=tdx
Environment=GAIKO2_TDXS_SOCKET=/var/tdxs.sock
Environment=GAIKO2_L2_RPC_URL=http://127.0.0.1:8545
Environment=GAIKO2_CONFIG_DIR=/persistent/gaiko2/config
Environment=GAIKO2_SECRET_DIR=/persistent/gaiko2/secrets
Environment=GAIKO2_FORK=shasta
ExecStart=/usr/bin/gaiko2 server
```

`taiko-geth` must expose JSON-RPC only on loopback or a VM-local socket. Do not
expose geth HTTP, WS, AuthRPC, or debug APIs outside the VM.

## Bootstrap And Registration

Bootstrap runs inside the measured VM:

```bash
GAIKO2_TEE_TYPE=tdx \
GAIKO2_TDXS_SOCKET=/var/tdxs.sock \
GAIKO2_CONFIG_DIR=/persistent/gaiko2/config \
GAIKO2_SECRET_DIR=/persistent/gaiko2/secrets \
gaiko2 bootstrap
```

The bootstrap output includes:

- public key / instance address
- TDX attestation document bound to the instance address
- nonce used for the quote

External registration should verify that quote through the selected TDX verifier
path, such as an Automata/Azure TDX verifier contract, and write
`registered.gaiko2.json` after the instance id is registered.

## Key And Image Identity

The private key is generated inside the VM and stored under `GAIKO2_SECRET_DIR`.
That directory must be on TDX-protected persistent storage, for example a
`tdx-init`/TPM-backed encrypted disk. The application does not make a normal host
volume trustworthy by itself.

The trusted image manifest must pin and record:

- gaiko2 git commit and binary sha256
- taiko-geth git commit and binary sha256
- taiko-client git commit and binary sha256
- tdxs git commit and binary sha256
- OS/kernel/initramfs/systemd unit hashes
- measured identity values used by the on-chain verifier

Changing any statement-affecting component must produce a new accepted image
identity and require a fresh key registration.

## Proof Path

For `POST /prove/shasta`, `tdxgeth` mode:

1. parses the standard `raiko2-shasta-request-v1` request
2. checks the request's block span and `proof_carry_data`
3. fetches matching headers from `GAIKO2_L2_RPC_URL`
4. rejects if local taiko-geth block hash, parent hash, state root, or receipts
   root differs from the request
5. signs the canonical Shasta input hash with the registered TDX key
6. returns `raiko2-proof-v1`

For `POST /prove/shasta-aggregate`, it reuses the standard aggregate validation
and signs the aggregate input hash with the same registered TDX key.

## Acceptance

Do not treat a TDX image as production-ready until:

- `gaiko2 bootstrap` succeeds inside the measured VM
- `gaiko2 check` can load the bootstrapped key
- `gaiko2 server` starts with `GAIKO2_PROVING_MODE=tdxgeth`
- `GET /healthz` is green
- remote prover proposal conformance passes
- remote prover aggregate conformance passes
- a real Shasta proposal regression succeeds through raiko2's explicit `tdxgeth`
  lane
