# gaiko2 SGX Docker Deployment Guide

This guide describes the intended production-style deployment flow for `gaiko2`
as a standalone SGX proving service.

The operator entry point is:

```bash
./scripts/deploy-tee.sh
```

The script hides the raw `docker compose` commands and keeps every deployed
release in its own directory, so bootstrap state and registration metadata are
not overwritten during upgrades.

## 1. Model

Each release lives under:

```text
deploy/<fork>/<release>/
  .env
  config/
    bootstrap.gaiko2.json
    registered.gaiko2.json
  secrets/
    priv.gaiko2.key
```

This is the key safety property of the deployment model:

- new release bootstrap does not overwrite the old release
- rollback is just starting the previous release again
- each release keeps its own image tag, port, hook config, and SGX state

`gaiko2` still uses one repo-root [compose.yaml](/home/yue/works/taiko/gaiko2/compose.yaml).
The deploy script selects a release by passing a release-specific:

- compose project name
- env file
- bind-mounted config/secrets directories

## 2. Host Prerequisites

The host must provide:

- SGX hardware with `/dev/sgx_enclave` and `/dev/sgx_provision`
- Docker and Docker Compose
- a reachable PCCS endpoint

If you already operate `raiko` SGX infrastructure, reuse the same PCCS setup.
The longer host-side PCCS instructions remain in:

- [raiko/docs/README_Docker_and_RA.md](/home/yue/works/taiko/raiko/docs/README_Docker_and_RA.md)
- [How to deploy SGX Server 2269673143d680798482d2ce9367f7c8.md](/home/yue/works/taiko/How%20to%20deploy%20SGX%20Server%202269673143d680798482d2ce9367f7c8.md)

## 3. Build or Select the Image

If you want a local image:

```bash
cd /home/yue/works/taiko/gaiko2
./scripts/build-image.sh tee latest
```

If you want a published image, note the tag and pass it to `init` with
`--tee-image`, for example:

```bash
ghcr.io/taikoxyz/gaiko2-tee:v1.0.0
```

## 4. Bootstrap a Release

Choose a fork and a release name. A release name is usually the image tag or an
operator-friendly alias such as `v1.0.0` or `2026-04-15-hotfix`.

Example:

```bash
cd /home/yue/works/taiko/gaiko2
./scripts/deploy-tee.sh \
  --fork shasta \
  --release v1.0.0 \
  --tee-image ghcr.io/taikoxyz/gaiko2-tee:v1.0.0 \
  --pccs-host host.docker.internal:8081 \
  init
```

What `init` does:

- creates `deploy/shasta/v1.0.0/`
- creates `deploy/shasta/v1.0.0/.env`
- creates `config/` and `secrets/`
- runs the tee bootstrap container

Expected result:

- `deploy/shasta/v1.0.0/config/bootstrap.gaiko2.json`
- `deploy/shasta/v1.0.0/secrets/priv.gaiko2.key`

The bootstrap JSON includes:

- `public_key`
- `new_instance`
- `quote`

If bootstrap fails, the command output is the primary log. Common failures:

- `Failed to get quote config` or `SGX_QL_NETWORK_ERROR`
  PCCS is unreachable or `--pccs-host` is wrong.
- `open /dev/sgx_enclave`
  SGX devices are unavailable on the host.

## 5. Register the Quote

`gaiko2` does not self-register on chain. Registration stays outside the server
binary.

### Option A: manual registration

Use your existing verifier registration flow with the quote from:

```bash
deploy/shasta/v1.0.0/config/bootstrap.gaiko2.json
```

Then write:

```json
{
  "shasta": 1234
}
```

to:

```bash
deploy/shasta/v1.0.0/config/registered.gaiko2.json
```

### Option B: register hook

Configure a hook while bootstrapping:

```bash
./scripts/deploy-tee.sh \
  --fork shasta \
  --release v1.0.0 \
  --register-hook /abs/path/to/register-hook.sh \
  init
```

Then invoke:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 register
```

An example hook contract is included at:

- [register-hook.example.sh](/home/yue/works/taiko/gaiko2/scripts/register-hook.example.sh)

The hook receives:

- `GAIKO2_BOOTSTRAP_JSON`
- `GAIKO2_REGISTERED_JSON`
- `GAIKO2_CONFIG_DIR`
- `GAIKO2_SECRET_DIR`
- `GAIKO2_FORK`
- `GAIKO2_RELEASE`

If no hook is configured, `register` prints the exact bootstrap and registered
JSON paths and exits without modifying state.

## 6. Start the Service

Once bootstrap and registration are complete:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 up
```

This runs:

- `docker compose --profile tee up -d --wait`

for that release only. The image healthcheck probes `/healthz`, so the command
waits until the container is healthy.

If startup fails, the script tells you to inspect logs with:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 logs
```

Expected healthy startup logs include:

- `listening on 0.0.0.0:8080`

## 7. Operational Commands

Check release status:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 status
```

This reports:

- deploy directory
- compose project name
- whether `.env` exists
- whether bootstrap exists
- whether the sealed key exists
- whether registered ids exist
- compose status for that release

Follow logs:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 logs
```

Check liveness:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 health
```

Expected:

```json
{"status":"ok"}
```

Stop and remove the release:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 down
```

## 8. Rollback

Because each release has its own directory, rollback is explicit and safe.

Example:

1. new release fails:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.1 down
```

2. start the previous release again:

```bash
./scripts/deploy-tee.sh --fork shasta --release v1.0.0 up
```

This restores the old release's exact:

- image tag
- `.env`
- sealed SGX key
- bootstrap quote metadata
- registered instance id mapping

No re-bootstrap is needed for rollback.

## 9. Example End-to-End Flow

```bash
cd /home/yue/works/taiko/gaiko2

./scripts/build-image.sh tee latest

./scripts/deploy-tee.sh \
  --fork shasta \
  --release local-latest \
  --tee-image gaiko2-tee:latest \
  --pccs-host host.docker.internal:8081 \
  init

# register externally, then either:
# 1. write deploy/shasta/local-latest/config/registered.gaiko2.json
# or
# 2. set GAIKO2_INSTANCE_ID in deploy/shasta/local-latest/.env

./scripts/deploy-tee.sh --fork shasta --release local-latest up
./scripts/deploy-tee.sh --fork shasta --release local-latest status
./scripts/deploy-tee.sh --fork shasta --release local-latest health
./scripts/deploy-tee.sh --fork shasta --release local-latest logs
```

## 10. Troubleshooting

### `status` says `bootstrap: missing`

Run:

```bash
./scripts/deploy-tee.sh --fork <fork> --release <release> init
```

### `up` fails because instance id is unresolved

Either:

- set `GAIKO2_INSTANCE_ID` in the release `.env`
- or write `registered.gaiko2.json`
- or configure and run `register`

### Healthcheck never becomes healthy

Inspect:

```bash
./scripts/deploy-tee.sh --fork <fork> --release <release> logs
```

Typical causes:

- wrong fork mapping in `registered.gaiko2.json`
- missing sealed key
- port already in use
- PCCS problems that prevented a valid bootstrap

### Need to inspect the generated release env

Open:

```bash
deploy/<fork>/<release>/.env
```

This file is the exact compose input for that release and should be preserved
for rollback and auditing.
