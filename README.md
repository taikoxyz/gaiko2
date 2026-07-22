# gaiko2

`gaiko2` is a lightweight Go service for replaying `raiko2` Shasta execution packets
with `taiko-geth` and producing a TEE proof envelope.

## Baseline

- `GET /healthz` returns a minimal liveness response.
- `/prove/shasta` accepts the soundness-oriented request
  `schema: "raiko2-shasta-request-v1"` with `payload.guest_input`.
- `/prove/shasta` rejects replay-only v1 packets without `payload.guest_input`.
  The legacy replay-packet wire shape is not a soundness-equivalent server API.
- `gaiko2` can decode `raiko2`-adapted execution packets and replay them with
  native `taiko-geth` stateless execution.
- For proposal requests, `gaiko2` validates `GuestInput` carry data, raw blob hashes,
  Shasta source manifests, canonical transaction lists, and block metadata before
  replaying the witness blocks.
- `gaiko2` validates replay continuity against `proof_carry_data`.
- proof output now supports two signer modes behind one envelope:
  - `native`: sign the final input hash with the fixed, published GoldenTouch
    mock key. Intended for local/dev only; the `/prove/shasta-aggregate`
    endpoint is disabled in native mode unless `GAIKO2_DEV_MODE=1` is set.
  - `tee`: sign with an enclave-managed key; the bootstrap step emits the `ego`
    quote used by external registration flows.
- the checked-in shared fixture under `testdata/` is derived from a real
  `raiko2` GuestInput fixture and replays successfully.

## Verification

Run the current native test suite with:

```bash
cd gaiko2
go test ./...
```

The main replay regression uses:

- [testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json](testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json)
- [internal/prover/replay_fixture_test.go](internal/prover/replay_fixture_test.go)

The GuestInput soundness checks are covered by:

- [internal/prover/guestinput_carry_test.go](internal/prover/guestinput_carry_test.go)
- [internal/prover/blob_validate_test.go](internal/prover/blob_validate_test.go)
- [internal/prover/manifest_validate_test.go](internal/prover/manifest_validate_test.go)
- [internal/prover/validate_test.go](internal/prover/validate_test.go)

## Server

Start the service with:

```bash
cd gaiko2
go run ./cmd/gaiko2 server
```

Probe liveness with:

```bash
curl http://127.0.0.1:8080/healthz
```

Both `/prove/shasta` and `/prove/shasta-aggregate` accept exactly one JSON
request value with a maximum body size of 512 MiB. Larger bodies return HTTP
`413` with the standard `REQUEST_TOO_LARGE` error envelope.

Optional proving configuration:

- `GAIKO2_PROVING_MODE=native|tee`
- `GAIKO2_TEE_TYPE=ego`
- `GAIKO2_CONFIG_DIR=/path/to/config`
- `GAIKO2_SECRET_DIR=/path/to/secrets`
- `GAIKO2_INSTANCE_ID=0xDEADC0DE`
- `GAIKO2_FORK=shasta`
- `GAIKO2_PORT=8080`
- `GAIKO2_DEV_MODE=1` (native mode only; see below)

If unset, `gaiko2` defaults to `native` mode.

Native mode signs with a published mock key, so it is intended for local
development only. Because `/prove/shasta-aggregate` produces the final on-chain
signature without executing any blocks, it is **disabled in native mode** and
returns HTTP `403` with an `AGGREGATE_DISABLED` error envelope; set
`GAIKO2_DEV_MODE=1` to re-enable it for local testing. The mock instance must
never be registered in a verifier guarding real value.

TEE mode expects the enclave key to be bootstrapped ahead of proving and also
requires either `GAIKO2_INSTANCE_ID` or a `GAIKO2_FORK` entry in
`registered.gaiko2.json`. `gaiko2 server` validates both requirements at
startup and caches the loaded key for later signing.

On `SIGINT` or `SIGTERM`, the server stops accepting new connections and gives
in-flight requests up to 30 seconds to finish. The checked-in Compose services
allow 35 seconds before forced termination so the application grace period can
complete.

Bootstrap the local tee state with:

```bash
cd gaiko2
GAIKO2_PROVING_MODE=tee GAIKO2_TEE_TYPE=ego \
  GAIKO2_CONFIG_DIR=/tmp/gaiko2-config \
  GAIKO2_SECRET_DIR=/tmp/gaiko2-secrets \
  go run ./cmd/gaiko2 bootstrap
```

The bootstrap command writes:

- `bootstrap.gaiko2.json` under `GAIKO2_CONFIG_DIR`
- `attestation.gaiko2.json` under `GAIKO2_CONFIG_DIR` when
  `GAIKO2_ATTESTATION_PATH` is available in the image
- `priv.gaiko2.key` under `GAIKO2_SECRET_DIR`

Re-running bootstrap without `--force` never rotates an existing tee key. If
the bootstrap JSON is missing, malformed, or identifies a different key, the
command rebuilds it from the existing sealed key and a fresh quote. Matching
metadata is left unchanged. If the sealed key cannot be loaded, for example
because it belongs to a different enclave identity, the error retains the
underlying cause and explains that `--force` can replace it.

To intentionally generate and install a replacement key, run:

```bash
GAIKO2_PROVING_MODE=tee GAIKO2_TEE_TYPE=ego \
  GAIKO2_CONFIG_DIR=/tmp/gaiko2-config \
  GAIKO2_SECRET_DIR=/tmp/gaiko2-secrets \
  go run ./cmd/gaiko2 bootstrap --force
```

Before replacement starts, the command writes a warning to stderr. Replacing
the key makes the old key and any on-chain registration bound to it unusable.

For tee Docker deployments, the container entrypoint copies the embedded
`attestation.json` into `GAIKO2_CONFIG_DIR/attestation.gaiko2.json` before the
enclave bootstrap runs.

If an external registration script writes `registered.gaiko2.json` under
`GAIKO2_CONFIG_DIR`, setting `GAIKO2_FORK=shasta` lets `gaiko2` resolve the tee
instance id from that file instead of requiring `GAIKO2_INSTANCE_ID` directly.

Inspect the embedded tee image metadata with:

```bash
GAIKO2_ATTESTATION_PATH=/opt/gaiko2/etc/attestation.json \
  go run ./cmd/gaiko2 metadata
```

## Docker

Build an image with:

```bash
cd gaiko2
./scripts/build-image.sh native latest
./scripts/build-image.sh tee latest
```

By default, `tee` image builds generate a disposable local enclave signing key
inside the Docker build and delete it after `ego sign`. That is useful for local
compile and smoke testing, but the resulting `signer_id` is not stable.

For release builds, fetch the MRSIGNER key from GCP Secret Manager and pass it to
Docker through a BuildKit secret:

```bash
GCP_ENCLAVE_KEY_SECRET=gaiko2-enclave-key \
GCP_ENCLAVE_KEY_VERSION=latest \
GCP_ENCLAVE_KEY_PROJECT=<gcp-project> \
ENCLAVE_KEY_PUBLIC_SHA256=<expected-public-key-sha256> \
  ./scripts/build-image.sh tee v1.0.0
```

`ENCLAVE_KEY_PUBLIC_SHA256` is optional, but should be set for release builds so
the Docker signing step rejects an unexpected MRSIGNER key. Do not place a PEM
file under `docker/`; local key files are ignored by the Docker build context.

Or directly:

```bash
docker buildx build . -f docker/Dockerfile.native --load --platform linux/amd64 -t gaiko2-native:latest
DOCKER_BUILDKIT=1 docker buildx build . -f docker/Dockerfile.tee --load --platform linux/amd64 -t gaiko2-tee:latest
```

Or use Compose profiles:

```bash
docker compose up --build
docker compose --profile tee-init run --rm gaiko2-tee-init
docker compose --profile tee up --build gaiko2-tee
```

For release-based SGX deployment, the operator entry point is:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 init
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 up
```

For the full runbook, including bootstrap, external registration, rollback, and
log-based troubleshooting, see:

- [SGX Docker deployment guide](https://github.com/taikoxyz/gaiko2/blob/main/docs/deployment/sgx-docker.md)

The compose file defaults `PCCS_HOST` to `host.docker.internal:8081` and adds the
host gateway mapping automatically, which works well when your local `pccs`
container already publishes `8081` on the host. If you run `gaiko2` on the same
Docker network as a `pccs` service, override `PCCS_HOST=pccs:8081` before
starting the tee profile.

Run the native server container on the default port:

```bash
docker run --rm -p 8080:8080 gaiko2-native:latest
```

The tee image also starts as a server and defaults to port `8080`, but it still
requires the usual SGX runtime devices and host configuration to run correctly.
Bootstrap the tee image explicitly before starting the server.

If you run the container on the same Docker network as a `pccs` service, the
default `PCCS_HOST=pccs:8081` works out of the box:

```bash
docker run --rm \
  --network docker_default \
  --device /dev/sgx_enclave \
  --device /dev/sgx_provision \
  -e GAIKO2_PROVING_MODE=tee \
  -v /path/to/config:/var/lib/gaiko2/config \
  -v /path/to/secrets:/var/lib/gaiko2/secrets \
  gaiko2-tee:latest --init

docker run --rm \
  --network docker_default \
  --device /dev/sgx_enclave \
  --device /dev/sgx_provision \
  -p 8080:8080 \
  -e GAIKO2_PROVING_MODE=tee \
  -v /path/to/config:/var/lib/gaiko2/config \
  -v /path/to/secrets:/var/lib/gaiko2/secrets \
  gaiko2-tee:latest
```

If you run `gaiko2-tee` outside that compose network, set `PCCS_HOST`
explicitly. For example, with host gateway routing:

```bash
docker run --rm \
  --add-host host.docker.internal:host-gateway \
  --device /dev/sgx_enclave \
  --device /dev/sgx_provision \
  -e GAIKO2_PROVING_MODE=tee \
  -e PCCS_HOST=host.docker.internal:8081 \
  -v /path/to/config:/var/lib/gaiko2/config \
  -v /path/to/secrets:/var/lib/gaiko2/secrets \
  gaiko2-tee:latest --init
```

## Docs

- [Shasta V1 design plan](https://github.com/taikoxyz/gaiko2/blob/main/docs/plans/2026-04-12-gaiko2-shasta-v1-design.md)
- [Shasta V1 implementation plan](https://github.com/taikoxyz/gaiko2/blob/main/docs/plans/2026-04-12-gaiko2-shasta-v1-implementation-plan.md)
- [SGX Docker deployment guide](https://github.com/taikoxyz/gaiko2/blob/main/docs/deployment/sgx-docker.md)
- [Current baseline](https://github.com/taikoxyz/gaiko2/blob/main/docs/baselines/2026-04-13-gaiko2-v1-baseline.md)
