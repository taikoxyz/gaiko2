# gaiko2 Release-Based TEE Deployment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a release-directory deployment script for `gaiko2` tee services so upgrades and rollbacks do not overwrite SGX bootstrap or registration state.

**Architecture:** Keep one repo-root `compose.yaml`, but drive it through a bash orchestrator that selects a release directory via `--env-file` and `--project-name`. Each release keeps its own `.env`, `config/`, and `secrets/` directories under `deploy/<fork>/<release>/`.

**Tech Stack:** Bash, Docker Compose, Go test

---

### Task 1: Add failing orchestration tests

**Files:**
- Create: `scripts/test-deploy-tee.sh`
- Test: `scripts/test-deploy-tee.sh`

**Step 1: Write failing tests**

Cover:
- `init` creates `deploy/<fork>/<release>/.env`, `config/`, and `secrets/`
- compose project name is `gaiko2-<fork>-<release-slug>`
- `status` reports missing bootstrap before init

**Step 2: Run test to verify it fails**

Run: `bash ./scripts/test-deploy-tee.sh`
Expected: FAIL because `deploy-tee.sh` does not exist yet.

**Step 3: Write minimal implementation**

Create `scripts/deploy-tee.sh` with:
- argument parsing
- release dir helpers
- `init`, `status`
- compose wrapper

**Step 4: Run test to verify it passes**

Run: `bash ./scripts/test-deploy-tee.sh`
Expected: PASS

**Step 5: Commit**

```bash
git add scripts/deploy-tee.sh scripts/test-deploy-tee.sh
git commit -m "feat: add release-based tee deploy script"
```

### Task 2: Wire compose and env isolation

**Files:**
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `.gitignore`

**Step 1: Remove fixed container names**

Update compose services so project-name isolation works.

**Step 2: Extend env template**

Add:
- `GAIKO2_REGISTER_HOOK`
- release-friendly default comments

**Step 3: Ignore local deploy state**

Ignore `deploy/` in git.

**Step 4: Verify compose config**

Run: `docker compose config`
Expected: PASS

**Step 5: Commit**

```bash
git add compose.yaml .env.example .gitignore
git commit -m "ops: isolate compose releases by project"
```

### Task 3: Add register hook and operational subcommands

**Files:**
- Modify: `scripts/deploy-tee.sh`
- Create: `scripts/register-hook.example.sh`

**Step 1: Add remaining subcommands**

Implement:
- `register`
- `up`
- `logs`
- `health`
- `down`

**Step 2: Add example register hook**

Create a documented example script showing the environment contract and how to
write `registered.gaiko2.json`.

**Step 3: Re-run shell tests**

Run: `bash ./scripts/test-deploy-tee.sh`
Expected: PASS

**Step 4: Verify help and health command flow**

Run:
- `bash ./scripts/deploy-tee.sh --help`
- `bash ./scripts/deploy-tee.sh --fork shasta --release demo status`

Expected: PASS

**Step 5: Commit**

```bash
git add scripts/deploy-tee.sh scripts/register-hook.example.sh
git commit -m "feat: add deploy subcommands and register hook"
```

### Task 4: Rewrite deployment docs around the script

**Files:**
- Modify: `docs/deployment/sgx-docker.md`
- Modify: `README.md`

**Step 1: Make the script the primary operator entry point**

Document:
- `init`
- `register`
- `up`
- `logs`
- `status`
- `health`
- `down`

**Step 2: Document rollback**

Show:
- stop bad release
- start old release

**Step 3: Final verification**

Run:
- `go test ./...`
- `bash ./scripts/test-deploy-tee.sh`
- `docker compose config`

Optional smoke:
- build image
- `deploy-tee.sh init`
- `deploy-tee.sh up`
- `deploy-tee.sh health`

**Step 4: Commit**

```bash
git add README.md docs/deployment/sgx-docker.md
git commit -m "docs: document release-based tee deployment"
```
