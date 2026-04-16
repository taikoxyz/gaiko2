# gaiko2 Attestation Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose and persist tee image attestation metadata so release tooling can read the SGX image identity without re-entering a container.

**Architecture:** Generate a small JSON metadata file during tee image build, ship it in the runtime image, expose it via a new `gaiko2 metadata` command, and copy it into the release config directory during bootstrap. Extend deploy/register tooling and docs to reference the new artifact.

**Tech Stack:** Go, Bash, Docker

---

### Task 1: Add failing Go tests for metadata command and persistence

**Files:**
- Modify: `cmd/gaiko2/main_test.go`
- Create: `internal/tee/attestation_test.go`

**Step 1: Write failing tests**

Cover:
- `gaiko2 metadata` dispatches to a metadata command handler
- bootstrap persistence can save/load `attestation.gaiko2.json`

**Step 2: Run tests to verify they fail**

Run:
- `go test ./cmd/gaiko2 -run 'TestRunMetadataDispatchesLifecycleCommand'`
- `go test ./internal/tee -run 'TestSaveAndLoadAttestationMetadata'`

**Step 3: Implement minimal metadata structs and command**

Add typed metadata load/save helpers and wire the CLI dispatch.

**Step 4: Re-run tests**

Expected: PASS

### Task 2: Embed tee attestation metadata in the image and bootstrap output

**Files:**
- Modify: `docker/Dockerfile.tee`
- Create: `internal/tee/attestation.go`
- Modify: `cmd/gaiko2/main.go`

**Step 1: Build a JSON metadata file inside the tee image**

Include:
- unique id
- signer id
- product id
- security version

**Step 2: Copy metadata into the mounted config dir during bootstrap**

Write:
- `attestation.gaiko2.json`

**Step 3: Add `gaiko2 metadata` command**

Print the embedded file to stdout.

**Step 4: Re-run Go tests**

Expected: PASS

### Task 3: Extend deploy/register tooling

**Files:**
- Modify: `scripts/deploy-tee.sh`
- Modify: `scripts/register-hook.example.sh`
- Modify: `scripts/test-deploy-tee.sh`

**Step 1: Treat attestation metadata as a release artifact**

Add:
- release path helper
- status output
- register hook export `GAIKO2_ATTESTATION_JSON`

**Step 2: Add shell regression coverage**

Verify:
- init produces or references the attestation artifact
- register hook receives the env var

**Step 3: Re-run shell tests**

Expected: PASS

### Task 4: Document the new artifact

**Files:**
- Modify: `docs/deployment/sgx-docker.md`
- Modify: `README.md`

**Step 1: Document `attestation.gaiko2.json`**

Explain:
- what it contains
- where it lives
- how registration tooling should use it

**Step 2: Final verification**

Run:
- `go test ./...`
- `bash ./scripts/test-deploy-tee.sh`
- `docker compose config`

Optional smoke:
- build tee image
- `deploy-tee.sh init`
- `gaiko2 metadata`

