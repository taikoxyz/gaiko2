# gaiko2

`gaiko2` is a lightweight Go service for replaying `raiko2` Shasta execution packets
with `taiko-geth` and producing a TEE proof envelope.

## Baseline

- `GET /healthz` returns a minimal liveness response.
- `/prove/shasta` is implemented with request `schema: "v1"`.
- `gaiko2` can decode `raiko2`-adapted execution packets and replay them with
  native `taiko-geth` stateless execution.
- `gaiko2` validates replay continuity against `proof_carry_data`.
- proof output now supports two signer modes behind one envelope:
  - `native`: sign the final input hash with the fixed GoldenTouch key.
  - `tee`: sign with an enclave-managed key; the bootstrap step emits the `ego`
    quote used by external registration flows.
- the checked-in shared fixture under `testdata/` is derived from a real
  `raiko2` GuestInput fixture and replays successfully.

## Verification

Run the current native test suite with:

```bash
cd /home/yue/works/taiko/gaiko2
go test ./...
```

The main replay regression uses:

- [testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json](/home/yue/works/taiko/gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json)
- [internal/prover/replay_fixture_test.go](/home/yue/works/taiko/gaiko2/internal/prover/replay_fixture_test.go)

## Server

Start the service with:

```bash
cd /home/yue/works/taiko/gaiko2
go run ./cmd/gaiko2 server
```

Probe liveness with:

```bash
curl http://127.0.0.1:8080/healthz
```

Optional proving configuration:

- `GAIKO2_PROVING_MODE=native|tee`
- `GAIKO2_TEE_TYPE=ego`
- `GAIKO2_CONFIG_DIR=/path/to/config`
- `GAIKO2_SECRET_DIR=/path/to/secrets`
- `GAIKO2_INSTANCE_ID=0xDEADC0DE`
- `GAIKO2_FORK=shasta`
- `GAIKO2_PORT=8080`

If unset, `gaiko2` defaults to `native` mode.

TEE mode expects the enclave key to be bootstrapped ahead of proving. `gaiko2 server`
checks that the sealed key is readable at startup and caches the loaded key for
later signing.

Bootstrap the local tee state with:

```bash
cd /home/yue/works/taiko/gaiko2
GAIKO2_PROVING_MODE=tee GAIKO2_TEE_TYPE=ego \
  GAIKO2_CONFIG_DIR=/tmp/gaiko2-config \
  GAIKO2_SECRET_DIR=/tmp/gaiko2-secrets \
  go run ./cmd/gaiko2 bootstrap
```

The bootstrap command writes:

- `bootstrap.gaiko2.json` under `GAIKO2_CONFIG_DIR`
- `priv.gaiko2.key` under `GAIKO2_SECRET_DIR`

If an external registration script writes `registered.gaiko2.json` under
`GAIKO2_CONFIG_DIR`, setting `GAIKO2_FORK=shasta` lets `gaiko2` resolve the tee
instance id from that file instead of requiring `GAIKO2_INSTANCE_ID` directly.

## Docker

Build an image with:

```bash
cd /home/yue/works/taiko/gaiko2
./scripts/build-image.sh native latest
./scripts/build-image.sh tee latest
```

Or directly:

```bash
docker build -f docker/Dockerfile.native -t gaiko2-native:latest .
docker build -f docker/Dockerfile.tee -t gaiko2-tee:latest .
```

Or use Compose profiles:

```bash
docker compose up --build
docker compose --profile tee-init run --rm gaiko2-tee-init
docker compose --profile tee up --build gaiko2-tee
```

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

- [Shasta V1 design plan](/home/yue/works/taiko/gaiko2/docs/plans/2026-04-12-gaiko2-shasta-v1-design.md)
- [Shasta V1 implementation plan](/home/yue/works/taiko/gaiko2/docs/plans/2026-04-12-gaiko2-shasta-v1-implementation-plan.md)
- [Current baseline](/home/yue/works/taiko/gaiko2/docs/baselines/2026-04-13-gaiko2-v1-baseline.md)
