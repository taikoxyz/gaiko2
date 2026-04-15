# gaiko2 SGX Docker Deployment Guide

This guide describes the operational path for running `gaiko2` as a standalone
SGX proving service in Docker.

The intended deployment model is:

1. prepare an SGX host and PCCS
2. bootstrap the enclave key once
3. register the resulting quote with an external script/tool
4. start the `gaiko2` tee server with Docker Compose

`gaiko2` does **not** self-register on chain. Registration stays outside the
server binary by design. The server only needs:

- a bootstrapped sealed key under `priv.gaiko2.key`
- either `GAIKO2_INSTANCE_ID` or a `registered.gaiko2.json` file

## 1. Host Prerequisites

The host must provide:

- SGX hardware with `/dev/sgx_enclave` and `/dev/sgx_provision`
- Docker and Docker Compose
- a reachable PCCS endpoint

If you already operate `raiko` SGX infrastructure, reuse the same PCCS setup.
The longer host-side PCCS instructions remain in:

- [raiko/docs/README_Docker_and_RA.md](/home/yue/works/taiko/raiko/docs/README_Docker_and_RA.md)
- [How to deploy SGX Server 2269673143d680798482d2ce9367f7c8.md](/home/yue/works/taiko/How%20to%20deploy%20SGX%20Server%202269673143d680798482d2ce9367f7c8.md)

## 2. Prepare the `gaiko2` Working Directory

Use the repo root as the compose working directory:

```bash
cd /home/yue/works/taiko/gaiko2
cp .env.example .env
mkdir -p var/config var/secrets
```

Edit `.env` and set at least:

- `GAIKO2_TEE_IMAGE`
  Use the published image tag you want to deploy. Keep the default only if you
  already built the image locally.
- `GAIKO2_FORK=shasta`
  The server uses this to resolve the registered instance id from
  `registered.gaiko2.json`.
- `PCCS_HOST`
  Use `host.docker.internal:8081` if your PCCS publishes `8081` on the host.
  Use `pccs:8081` if you deploy `gaiko2` into the same Docker network as a PCCS
  container named `pccs`.

If you want to bypass `registered.gaiko2.json`, you may set
`GAIKO2_INSTANCE_ID=<number>` directly.

## 3. Bootstrap the TEE Key

Run the init container once:

```bash
docker compose --profile tee-init run --rm gaiko2-tee-init
```

Expected result:

- the command exits `0`
- `var/secrets/priv.gaiko2.key` exists
- `var/config/bootstrap.gaiko2.json` exists

The bootstrap output JSON includes:

- `public_key`
- `new_instance`
- `quote`

If bootstrap fails, inspect:

```bash
docker compose --profile tee-init logs gaiko2-tee-init
```

Common failures:

- `Failed to get quote config` or `SGX_QL_NETWORK_ERROR`
  `PCCS_HOST` is wrong or PCCS is unreachable.
- `open /dev/sgx_enclave`
  the host SGX devices are missing or not passed through.

## 4. Register the Quote Externally

`gaiko2` does not register itself. Use your existing verifier registration flow,
the same way `raiko` does for SGX services.

Input to that external registration step:

- the quote from `var/config/bootstrap.gaiko2.json`
- the verifier address for the target fork
- the operator wallet / L1 RPC

After registration, store the resulting instance id in
`var/config/registered.gaiko2.json`:

```json
{
  "shasta": 1234
}
```

If you prefer, set `GAIKO2_INSTANCE_ID=1234` in `.env` instead and skip the
JSON file.

## 5. One-Command Server Deployment

After bootstrap and registration are done, the steady-state deployment command
is:

```bash
docker compose --profile tee up -d --wait gaiko2-tee
```

Why this works:

- the tee image now includes a container healthcheck against `/healthz`
- Compose waits until the container becomes healthy
- the service restarts automatically because the compose service uses
  `restart: unless-stopped`

If the command does not become healthy, inspect:

```bash
docker compose logs -f --tail=200 gaiko2-tee
```

Expected healthy startup logs include:

- `listening on 0.0.0.0:8080`

Typical startup failures are explicit:

- `registered instance id for fork "shasta" not found`
  `registered.gaiko2.json` is missing or does not contain the configured fork.
- `no such file or directory` for `priv.gaiko2.key`
  bootstrap has not been run or the wrong secret volume is mounted.
- `bind: address already in use`
  the host port is already occupied.

## 6. Health and Proof Smoke Checks

Check liveness:

```bash
curl http://127.0.0.1:${GAIKO2_TEE_PORT:-8080}/healthz
```

Expected:

```json
{"status":"ok"}
```

Send a proof request:

```bash
curl -sS -X POST \
  -H 'Content-Type: application/json' \
  --data-binary @/home/yue/works/taiko/gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json \
  http://127.0.0.1:${GAIKO2_TEE_PORT:-8080}/prove/shasta
```

## 7. Operational Commands

Start:

```bash
docker compose --profile tee up -d --wait gaiko2-tee
```

Follow logs:

```bash
docker compose logs -f --tail=200 gaiko2-tee
```

Stop:

```bash
docker compose --profile tee stop gaiko2-tee
```

Restart:

```bash
docker compose --profile tee restart gaiko2-tee
```

Remove the server container:

```bash
docker compose --profile tee rm -sf gaiko2-tee
```

Re-bootstrap:

```bash
docker compose --profile tee-init run --rm gaiko2-tee-init
```

Do **not** re-bootstrap an existing production instance unless you intend to
replace the enclave key and re-register a new on-chain instance id.

## 8. Using a Published Image Instead of Local Build

Set the image name in `.env`, for example:

```bash
GAIKO2_TEE_IMAGE=ghcr.io/taikoxyz/gaiko2-tee:v1.0.0
```

Then pull and start:

```bash
docker compose pull gaiko2-tee
docker compose --profile tee up -d --wait gaiko2-tee
```

For local development or local release testing, build first:

```bash
./scripts/build-image.sh tee latest
docker compose --profile tee up -d --wait gaiko2-tee
```

## 9. Troubleshooting Checklist

### Container never becomes healthy

Check:

```bash
docker compose ps
docker compose logs -f --tail=200 gaiko2-tee
```

### Bootstrap succeeded, but server exits immediately

Check:

- `GAIKO2_FORK`
- `GAIKO2_INSTANCE_ID`
- `var/config/registered.gaiko2.json`

The server must be able to resolve an instance id before serving tee proofs.

### PCCS resolution errors

If you see hostname or quote collateral errors:

- use `PCCS_HOST=host.docker.internal:8081` when PCCS publishes to the host
- use `PCCS_HOST=pccs:8081` when both services share the same Docker network
- make sure compose includes the `host-gateway` mapping when using
  `host.docker.internal`

### Want native instead of tee

Run:

```bash
docker compose up -d --wait gaiko2-native
```

The native service uses the same `/healthz` and `/prove/shasta` routes, but it
signs with the GoldenTouch development key instead of an enclave-managed key.
