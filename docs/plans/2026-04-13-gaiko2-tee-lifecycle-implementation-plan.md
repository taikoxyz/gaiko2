# Gaiko2 Tee Lifecycle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add explicit `bootstrap` and `check` lifecycle commands for `gaiko2` tee deployments, plus Docker entrypoint support for `--init` and stable loading of externally registered instance ids.

**Architecture:** Keep proving stateless and side-effect free. `gaiko2` owns tee key bootstrap, quote generation, and local lifecycle metadata. Chain registration stays outside the server binary; `gaiko2` only reads the resulting `registered.gaiko2.json` artifact to resolve fork-specific instance ids.

**Tech Stack:** Go CLI, native tests, Docker, `ego`/SGX attestation, existing `taiko-geth` proving path

---

### Task 1: Pin lifecycle behavior with tests

**Files:**
- Modify: `cmd/gaiko2/main_test.go`
- Create: `internal/tee/bootstrap_test.go`
- Create: `internal/prover/config_test.go`

**Step 1: Write failing tests**
- Add CLI tests for `gaiko2 bootstrap` and `gaiko2 check`.
- Add tee storage tests that verify bootstrap metadata and registered fork ids are persisted under stable filenames.
- Add tests that ensure server mode fails when tee state is missing instead of silently creating keys.
- Add tests that ensure fork-based instance id lookup reads `registered.gaiko2.json`.

**Step 2: Run tests to verify they fail**
- Run: `go test ./cmd/gaiko2 ./internal/tee ./internal/prover`
- Expected: failures for missing commands/types/helpers.

### Task 2: Implement tee bootstrap persistence

**Files:**
- Modify: `internal/tee/provider.go`
- Modify: `internal/tee/ego.go`
- Create: `internal/tee/bootstrap.go`

**Step 1: Write minimal implementation**
- Add stable filenames for `priv.gaiko2.key`, `bootstrap.gaiko2.json`, and `registered.gaiko2.json`.
- Add bootstrap data struct and JSON save/load helpers.
- Add explicit helpers to save/load registered fork ids.

**Step 2: Run targeted tests**
- Run: `go test ./internal/tee`
- Expected: bootstrap persistence tests pass.

### Task 3: Implement explicit lifecycle commands

**Files:**
- Modify: `cmd/gaiko2/main.go`
- Modify: `internal/prover/config.go`
- Modify: `internal/prover/signer.go`

**Step 1: Write minimal implementation**
- Add `bootstrap` and `check` commands.
- `bootstrap` generates a tee key, derives address, fetches quote, and saves bootstrap JSON.
- `check` verifies the sealed key is readable.
- Make tee proving require a pre-existing sealed key.
- Load registered fork ids from `registered.gaiko2.json` when `GAIKO2_FORK` is set.

**Step 2: Run targeted tests**
- Run: `go test ./cmd/gaiko2 ./internal/prover`
- Expected: lifecycle command tests pass.

### Task 4: Wire Docker tee init flow

**Files:**
- Modify: `docker/Dockerfile.tee`
- Create: `docker/entrypoint.sh`
- Modify: `README.md`

**Step 1: Write minimal implementation**
- Add tee entrypoint script supporting default server and `--init`.
- Document that registration is handled by an external script/tool which writes `registered.gaiko2.json`.
- Keep native image behavior unchanged.

**Step 2: Verify scripts/docs**
- Run: `bash -n scripts/build-image.sh`
- Run: `bash -n docker/entrypoint.sh`
- Expected: no shell errors.

### Task 5: Verify end-to-end behavior

**Files:**
- Modify if needed: `README.md`

**Step 1: Run full native verification**
- Run: `go test ./...`
- Expected: all tests pass.

**Step 2: Run Docker verification**
- Build: `./scripts/build-image.sh native local`
- Build: `./scripts/build-image.sh tee local`
- Smoke: start `gaiko2-native:local` and POST the checked-in Shasta fixture to `/prove/shasta`
- Expected: HTTP 200 with `status=ok`

**Step 3: Commit**
- Commit Docker/lifecycle work after verification passes.
