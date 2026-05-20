# gaiko2 Remote Prover Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename the `gaiko2` remote prover protocol surface to the `raiko2`-owned schema names without changing route names, payload layout, replay behavior, or TEE behavior.

**Architecture:** Keep the existing `protocol`, `api`, and `prover` package boundaries intact, but split schema constants so proposal requests, aggregate requests, and proof responses each have their own canonical name. Then update validations, fixtures, tests, and documentation in lockstep and verify both local unit tests and the `raiko2` black-box conformance harness.

**Tech Stack:** Go, `net/http`, JSON fixtures, Go test, Cargo test

---

### Task 1: Split Protocol Schema Constants

**Files:**
- Modify: `internal/protocol/shasta_v1.go`
- Test: `internal/protocol/shasta_v1_test.go`

**Step 1: Write the failing tests**

Update or add assertions so the protocol tests expect:

- proposal request schema `raiko2-shasta-request-v1`
- aggregate request schema `raiko2-shasta-aggregate-request-v1`
- proof response schema `raiko2-proof-v1`

**Step 2: Run test to verify it fails**

Run: `go test ./internal/protocol -run 'Test(ShastaV1RoundTrip|ShastaAggregateV1RoundTrip|ProofResponseHelpers)'`
Expected: FAIL because the code still emits and expects the old schema names.

**Step 3: Write minimal implementation**

In `internal/protocol/shasta_v1.go`:

- replace the shared request schema constant with separate proposal and aggregate constants,
- rename the response schema constant to `raiko2-proof-v1`,
- keep request and response struct layouts unchanged.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/protocol -run 'Test(ShastaV1RoundTrip|ShastaAggregateV1RoundTrip|ProofResponseHelpers)'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/protocol/shasta_v1.go internal/protocol/shasta_v1_test.go
git commit -m "feat(protocol): rename remote prover schemas"
```

### Task 2: Update Proposal Request Validation

**Files:**
- Modify: `internal/prover/validate.go`
- Modify: `internal/prover/validate_test.go`

**Step 1: Write the failing tests**

Update proposal request tests so valid packets use `raiko2-shasta-request-v1`, and unsupported schema tests reject the old `"v1"` name.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/prover -run 'TestValidateRequest'`
Expected: FAIL because validation still compares against the old shared request schema.

**Step 3: Write minimal implementation**

In `internal/prover/validate.go`:

- make `ValidateRequest` compare against the new proposal request schema constant only.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/prover -run 'TestValidateRequest'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prover/validate.go internal/prover/validate_test.go
git commit -m "feat(prover): accept raiko2 proposal schema"
```

### Task 3: Update Aggregate Request Validation

**Files:**
- Modify: `internal/prover/aggregate_validate.go`
- Modify: `internal/prover/aggregate_test.go`

**Step 1: Write the failing tests**

Update aggregate request tests so valid aggregate packets use `raiko2-shasta-aggregate-request-v1`, and unsupported schema tests reject the old `"v1"` name.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/prover -run 'Test(ReplayServiceReturnsSignedAggregationProofResult|ValidateAggregateRequest)'`
Expected: FAIL because aggregate validation still compares against the old shared request schema.

**Step 3: Write minimal implementation**

In `internal/prover/aggregate_validate.go`:

- make `ValidateAggregateRequest` compare against the new aggregate request schema constant only.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/prover -run 'Test(ReplayServiceReturnsSignedAggregationProofResult|ValidateAggregateRequest)'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/prover/aggregate_validate.go internal/prover/aggregate_test.go
git commit -m "feat(prover): accept raiko2 aggregate schema"
```

### Task 4: Update Shared Fixture and API Tests

**Files:**
- Modify: `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`
- Modify: `internal/api/server_test.go`
- Modify: `internal/prover/replay_fixture_test.go`
- Modify: `testdata/README.md`

**Step 1: Write the failing tests**

Update the fixture-driven tests so they expect the shared request fixture schema to be `raiko2-shasta-request-v1`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api ./internal/prover -run 'Test(NewServerReturnsSuccessEnvelope|SharedShastaFixtureMetadata)'`
Expected: FAIL because the checked-in fixture still says `"v1"`.

**Step 3: Write minimal implementation**

- update the shared fixture schema string,
- update fixture metadata assertions,
- update `testdata/README.md` to describe the new schema name.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api ./internal/prover -run 'Test(NewServerReturnsSuccessEnvelope|SharedShastaFixtureMetadata)'`
Expected: PASS

**Step 5: Commit**

```bash
git add testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json internal/api/server_test.go internal/prover/replay_fixture_test.go testdata/README.md
git commit -m "test: update shared remote prover fixture schema"
```

### Task 5: Synchronize Deployment and Regression Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/deployment/masaya-fork-window-regression.md`

**Step 1: Update schema references**

Replace the old protocol names in the user-facing docs so they no longer describe:

- request schema `"v1"`
- response schema `"gaiko2-proof-v1"`

They should describe the new `raiko2`-owned schema names instead.

**Step 2: Run a quick search verification**

Run: `rg -n '"v1"|gaiko2-proof-v1|raiko2-proof-v1|raiko2-shasta-request-v1|raiko2-shasta-aggregate-request-v1' README.md docs testdata internal`
Expected:
- code and fixtures use the new names where protocol constants are asserted,
- old names remain only in historical design/baseline documents where intentionally preserved.

**Step 3: Commit**

```bash
git add README.md docs/deployment/masaya-fork-window-regression.md
git commit -m "docs: update remote prover schema references"
```

### Task 6: Run Full gaiko2 Verification

**Files:**
- Modify: none

**Step 1: Run focused Go verification**

Run: `go test ./internal/api ./internal/prover ./internal/protocol ./cmd/gaiko2`
Expected: PASS

**Step 2: Run repo-wide sanity check if the focused suite passes**

Run: `go test ./...`
Expected: PASS, or explicitly note any unrelated pre-existing failures.

**Step 3: Commit verification-only checkpoint if needed**

```bash
git status --short
```

Expected: only intended tracked files remain changed.

### Task 7: Run raiko2 Black-Box Conformance

**Files:**
- Modify: none

**Step 1: Start the local gaiko2 service**

Run:

```bash
go run ./cmd/gaiko2 server :8080
```

Expected: local server listens on `127.0.0.1:8080` or `0.0.0.0:8080`.

**Step 2: Run the conformance harness from the local raiko2 checkout**

Run:

```bash
cd <raiko2-checkout>
RAIKO2_REMOTE_PROVER_BASE_URL=http://127.0.0.1:8080 \
cargo test -p raiko2-prover --no-default-features \
  --test remote_prover_conformance -- --ignored --nocapture
```

Expected:

- `remote_prover_conformance_proposal` passes
- `remote_prover_conformance_aggregate` passes

**Step 3: Record any remaining gaps**

If the conformance harness cannot be run locally, capture the exact blocker and stop without guessing.
