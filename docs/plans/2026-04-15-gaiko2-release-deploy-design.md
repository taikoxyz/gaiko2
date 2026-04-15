# gaiko2 Release-Based TEE Deployment Design

## Goal

Add a release-oriented deployment workflow for `gaiko2` tee services so each
deployed version owns its own immutable local state and can be rolled back
without overwriting another release's bootstrap key, registration metadata, or
environment configuration.

## Problem

The current compose flow is single-directory oriented:

- one `.env`
- one `var/config`
- one `var/secrets`

This is unsafe for SGX service upgrades because a fresh bootstrap for a new
image version would overwrite:

- `priv.gaiko2.key`
- `bootstrap.gaiko2.json`
- `registered.gaiko2.json`

That breaks rollback semantics and makes it hard to reason about which on-chain
instance id belongs to which deployed image.

## Deployment Model

Use immutable release directories under a stable deploy root:

```text
deploy/<fork>/<release>/
  .env
  config/
    bootstrap.gaiko2.json
    registered.gaiko2.json
  secrets/
    priv.gaiko2.key
```

Each release directory is self-contained and survives later releases.

`gaiko2` keeps a single `compose.yaml` in the repo root. The deployment script
selects the release by passing:

- `--project-name gaiko2-<fork>-<release>`
- `--env-file deploy/<fork>/<release>/.env`

This avoids copying compose files per release while still isolating state.

## Script Interface

Add one orchestrator script:

```bash
./scripts/deploy-tee.sh init
./scripts/deploy-tee.sh register
./scripts/deploy-tee.sh up
./scripts/deploy-tee.sh logs
./scripts/deploy-tee.sh status
./scripts/deploy-tee.sh health
./scripts/deploy-tee.sh down
```

Global selectors:

- `--fork shasta`
- `--release v1.0.0`
- optional `--deploy-root ./deploy`

`init` creates the release directory and `.env` if missing, applies any
command-line overrides, then runs `docker compose --profile tee-init run --rm`.

`register` optionally runs an external hook. `gaiko2` itself still does not do
chain registration.

`up` starts the tee service for the selected release with
`docker compose --profile tee up -d --wait`.

`down` stops and removes the selected release's compose project.

## Register Hook Contract

The deploy script may call an optional external hook configured by
`GAIKO2_REGISTER_HOOK`.

The hook receives:

- `GAIKO2_BOOTSTRAP_JSON`
- `GAIKO2_REGISTERED_JSON`
- `GAIKO2_CONFIG_DIR`
- `GAIKO2_SECRET_DIR`
- `GAIKO2_FORK`
- `GAIKO2_RELEASE`
- `GAIKO2_DEPLOY_DIR`

Success means the hook exits `0`. The normal expectation is that it writes
`registered.gaiko2.json`. If no hook is configured, the script prints the exact
path to the bootstrap file and the expected next step.

## Compose Changes

`compose.yaml` must stop using fixed `container_name` values, otherwise multiple
release projects cannot coexist in Docker metadata.

Image names stay configurable through:

- `GAIKO2_TEE_IMAGE`
- `GAIKO2_NATIVE_IMAGE`

## Rollback Semantics

Rollback is explicit and directory-based:

1. stop the bad release:
   `./scripts/deploy-tee.sh --fork shasta --release new down`
2. start the last known-good release:
   `./scripts/deploy-tee.sh --fork shasta --release old up`

This keeps the host port the same while restoring the old release's exact key,
registration state, and environment.

## Error Handling

The script should fail fast and point operators to the next useful command:

- `up` failure -> suggest `logs`
- missing bootstrap -> suggest `init`
- missing instance id / registration -> suggest `register`

`status` should also summarize:

- deploy directory
- `.env` presence
- bootstrap file presence
- sealed key presence
- registered file presence
- compose service status

## Testing

Because the main logic is in bash orchestration, add a lightweight shell test
that stubs compose calls and verifies:

- release directory creation
- `.env` generation
- project naming
- subcommand routing

Then keep the existing Go tests and compose config validation as integration
checks.
