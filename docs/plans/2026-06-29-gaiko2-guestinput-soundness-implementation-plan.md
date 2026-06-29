# Gaiko2 GuestInput Soundness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `gaiko2` proposal proving consume and validate a full Shasta `GuestInput` instead of the replay-only v1 packet.

**Architecture:** Add a v2 proposal request schema whose payload contains a `GuestInput`-equivalent JSON object. Validate proposal/blob/manifest/carry bindings before converting the validated input into the existing `taiko-geth` replay runner. Keep aggregation based on child proof carry data and signatures.

**Tech Stack:** Go, taiko-geth, JSON, Go test, Rust raiko2 fixture generation, HTTP conformance tests

---

### Task 1: Add V2 Protocol Types

**Files:**
- Modify: `internal/protocol/shasta_v1.go`
- Create: `internal/protocol/shasta_v2.go`
- Create: `internal/protocol/shasta_v2_test.go`

**Step 1: Write the failing protocol tests**

Add tests for:

- `ShastaRequestSchemaV2 == "raiko2-shasta-request-v2"`
- v2 request JSON has `payload.guest_input`
- v2 decoding preserves `witnesses`, `taiko`, `proposal_ancestor_headers`, `proposal_state_nodes`, and `proof_carry_data`
- v1 and aggregate schema constants remain unchanged

Run:

```bash
go test ./internal/protocol -run 'TestShastaV2'
```

Expected: FAIL because v2 types do not exist.

**Step 2: Implement protocol structs**

Add:

```go
const ShastaRequestSchemaV2 = "raiko2-shasta-request-v2"

type ShastaRequestV2 struct {
    Schema  string          `json:"schema"`
    Payload ShastaPayloadV2 `json:"payload"`
}

type ShastaPayloadV2 struct {
    GuestInput ShastaGuestInput `json:"guest_input"`
}
```

Define `ShastaGuestInput` with the minimum typed fields needed by validation. Use `json.RawMessage` only for subtrees that are immediately decoded by a narrower helper in the same task or a later task.

**Step 3: Verify protocol tests pass**

Run:

```bash
go test ./internal/protocol -run 'TestShastaV2'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/protocol/shasta_v1.go internal/protocol/shasta_v2.go internal/protocol/shasta_v2_test.go
git commit -m "feat(protocol): add shasta guestinput request schema"
```

### Task 2: Decode GuestInput Into Replay-Compatible Views

**Files:**
- Create: `internal/prover/guestinput.go`
- Create: `internal/prover/guestinput_test.go`

**Step 1: Write the failing decode tests**

Use a small JSON fixture with:

- one witness,
- `taiko.proposal_event`,
- `taiko.data_sources`,
- `proof_carry_data`.

Test that decoding extracts:

- chain id,
- witness block header number/hash/parent hash/state root,
- proposal id/hash inputs,
- data source count,
- carry checkpoint.

Run:

```bash
go test ./internal/prover -run 'TestDecodeGuestInput'
```

Expected: FAIL because the decoder does not exist.

**Step 2: Implement the decoder**

Add a `GuestInputView` that keeps typed fields for validation and keeps the original witness data for replay conversion.

Do not validate soundness in this task. Only decode and normalize obvious Go types such as hashes, addresses, quantities, headers, and blocks.

**Step 3: Verify decode tests pass**

Run:

```bash
go test ./internal/prover -run 'TestDecodeGuestInput'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/prover/guestinput.go internal/prover/guestinput_test.go
git commit -m "feat(prover): decode shasta guestinput payload"
```

### Task 3: Validate Carry Is Derived From GuestInput

**Files:**
- Modify: `internal/prover/guestinput.go`
- Create: `internal/prover/guestinput_carry_test.go`

**Step 1: Write failing carry mismatch tests**

Add table tests that mutate one field at a time and expect rejection:

- `proof_carry_data.chain_id`
- `proposal_id`
- `proposal_hash`
- `parent_proposal_hash`
- `parent_block_hash`
- `actual_prover`
- proposer
- timestamp
- checkpoint block number
- checkpoint block hash
- checkpoint state root
- verifier

Run:

```bash
go test ./internal/prover -run 'TestGuestInputCarry'
```

Expected: FAIL because carry validation does not exist.

**Step 2: Implement carry validation**

Implement a helper equivalent to `raiko2_primitives_shasta::build_proof_carry_data`:

- select chain id from first witness chain spec or Taiko chain spec,
- resolve verifier from witness chain spec for the `gaiko2` proof type,
- hash the proposal using the same Shasta proposal hash rules,
- derive checkpoint from the last witness block,
- compare the derived carry to the supplied carry.

**Step 3: Verify tests pass**

Run:

```bash
go test ./internal/prover -run 'TestGuestInputCarry'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/prover/guestinput.go internal/prover/guestinput_carry_test.go
git commit -m "feat(prover): validate shasta carry from guestinput"
```

### Task 4: Validate Blob Sources From Raw Blob Bytes

**Files:**
- Create: `internal/prover/blob_validate.go`
- Create: `internal/prover/blob_validate_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Write failing blob validation tests**

Add tests for:

- valid raw blob computes the expected proposal versioned hash,
- missing blob data is rejected for blob-backed proposal source,
- blob count mismatch is rejected,
- raw blob mutation is rejected even if caller-supplied commitment/proof is unchanged,
- caller-supplied commitment/proof alone is not trusted.

Run:

```bash
go test ./internal/prover -run 'TestValidateBlobSources'
```

Expected: FAIL because blob validation does not exist.

**Step 2: Implement direct KZG validation**

Use taiko-geth/geth KZG helpers:

- convert raw bytes to `kzg4844.Blob`,
- compute commitment from the raw blob,
- compute versioned hash from the computed commitment,
- compare with proposal `blobSlice.blobHashes`.

Do not use zk proof-of-equivalence in `gaiko2`.

**Step 3: Verify tests pass**

Run:

```bash
go test ./internal/prover -run 'TestValidateBlobSources'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/prover/blob_validate.go internal/prover/blob_validate_test.go go.mod go.sum
git commit -m "feat(prover): validate shasta blobs from raw bytes"
```

### Task 5: Bind Derived Manifest To Witness Blocks

**Files:**
- Create: `internal/prover/manifest_validate.go`
- Create: `internal/prover/manifest_validate_test.go`
- Modify: `internal/prover/guestinput.go`

**Step 1: Write failing manifest mismatch tests**

Add tests that reject:

- derived block count differing from witness count,
- non-anchor transaction mismatch,
- missing anchor transaction,
- timestamp mismatch,
- coinbase mismatch,
- gas limit mismatch,
- extra data mismatch,
- difficulty or mix hash mismatch,
- anchor recipient mismatch,
- anchor checkpoint mismatch.

Run:

```bash
go test ./internal/prover -run 'TestValidateManifestBinding'
```

Expected: FAIL because manifest validation does not exist.

**Step 2: Implement manifest derivation and binding**

Preferred path:

- import or extract a reusable Taiko Go helper for Shasta source manifest decoding.

Fallback path:

- implement the minimum Shasta source manifest decoding in `gaiko2`,
- keep it isolated in `manifest_validate.go`,
- cover it with golden vectors generated by `raiko2`.

Then compare each derived block manifest to the witness block:

- `len(block.Transactions()) == len(manifest.Transactions) + 1`
- RLP/canonical transaction bytes match for non-anchor transactions,
- header metadata matches protocol rules,
- anchor transaction fields and anchor checkpoint match.

**Step 3: Verify tests pass**

Run:

```bash
go test ./internal/prover -run 'TestValidateManifestBinding'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go internal/prover/guestinput.go
git commit -m "feat(prover): bind shasta manifest to replay blocks"
```

### Task 6: Add V2 Proposal Validation And Replay Conversion

**Files:**
- Modify: `internal/prover/validate.go`
- Modify: `internal/prover/replay.go`
- Modify: `internal/prover/validate_test.go`
- Create: `internal/prover/validate_v2_test.go`

**Step 1: Write failing v2 validation tests**

Add tests for:

- valid v2 `GuestInput` request is accepted,
- v1 replay-only request is rejected in production mode,
- v1 can only be accepted if an explicit unsafe compatibility option is enabled, if that option is kept,
- validated `GuestInput` converts into the same replay block view expected by the existing runner.

Run:

```bash
go test ./internal/prover -run 'TestValidateRequestV2|TestValidateRequestRejectsReplayOnlyV1'
```

Expected: FAIL because `ValidateRequest` only supports v1.

**Step 2: Implement v2 validation pipeline**

Validation order:

1. schema check,
2. decode `GuestInput`,
3. validate witness/block continuity,
4. validate blob sources,
5. validate manifest binding,
6. validate carry binding,
7. convert validated `GuestInput` to replay blocks,
8. return a `ValidatedRequest` compatible with existing replay service internals.

**Step 3: Verify tests pass**

Run:

```bash
go test ./internal/prover -run 'TestValidateRequestV2|TestValidateRequestRejectsReplayOnlyV1'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/prover/validate.go internal/prover/replay.go internal/prover/validate_test.go internal/prover/validate_v2_test.go
git commit -m "feat(prover): validate guestinput proposal requests"
```

### Task 7: Update API Handler To Decode V2 Requests

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `testdata/README.md`

**Step 1: Write failing API tests**

Add tests that POST a v2 request to `/prove/shasta` and assert:

- success envelope keeps `schema = "raiko2-proof-v1"`,
- response `input` matches the Shasta subproof input hash,
- v1 request is rejected unless explicitly enabled.

Run:

```bash
go test ./internal/api -run 'TestServerShastaV2'
```

Expected: FAIL because the API only decodes v1 request structs.

**Step 2: Implement API request dispatch**

Decode the envelope schema first, then dispatch to:

- v2 `GuestInput` validation path for production,
- optional v1 compatibility path only if configured.

Keep response envelope unchanged.

**Step 3: Verify tests pass**

Run:

```bash
go test ./internal/api -run 'TestServerShastaV2'
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go testdata/README.md
git commit -m "feat(api): accept shasta guestinput requests"
```

### Task 8: Update Raiko2 Gaiko2 Adapter

**Files:**
- Modify in `raiko2`: `crates/prover/src/remote_prover/protocol.rs`
- Modify in `raiko2`: `crates/prover/src/remote_prover/adapter.rs`
- Modify in `raiko2`: `crates/prover/tests/gaiko2_adapter.rs`
- Modify in `raiko2`: `crates/prover/tests/gaiko2_shared_fixture.rs`
- Modify in `raiko2`: `crates/prover/tests/remote_prover_conformance.rs`

**Step 1: Write failing adapter tests**

Update adapter tests to expect:

- schema `raiko2-shasta-request-v2`,
- `payload.guest_input` containing full `GuestInput`,
- no replay-only `payload.blocks` at the top level.

Run from the `raiko2` checkout:

```bash
cargo test -p raiko2-prover --test gaiko2_adapter -- --nocapture
```

Expected: FAIL because the adapter still sends v1 replay-only packets.

**Step 2: Implement adapter change**

Change `build_shasta_packet` so it sends a v2 payload containing `GuestInput`.

Do not drop compacted witness data. The serialized value must preserve:

- witnesses,
- Taiko manifest,
- proposal ancestor headers,
- proposal state nodes,
- proof carry data.

**Step 3: Verify adapter tests pass**

Run:

```bash
cargo test -p raiko2-prover --test gaiko2_adapter -- --nocapture
```

Expected: PASS.

**Step 4: Commit**

```bash
git add crates/prover/src/remote_prover/protocol.rs crates/prover/src/remote_prover/adapter.rs crates/prover/tests/gaiko2_adapter.rs crates/prover/tests/gaiko2_shared_fixture.rs crates/prover/tests/remote_prover_conformance.rs
git commit -m "feat(prover): send gaiko2 guestinput requests"
```

### Task 9: Add Cross-Repo Golden Fixture

**Files:**
- Create or update in `gaiko2`: `testdata/shasta_guestinput_*.json`
- Create or update in `gaiko2`: `internal/prover/guestinput_fixture_test.go`
- Create or update in `raiko2`: `crates/prover/tests/fixtures/*`

**Step 1: Generate a real Shasta `GuestInput` fixture**

Use the existing `raiko2` preflight path to generate a small Shasta proposal `GuestInput` with blob-backed data source.

Record:

- full `GuestInput` JSON,
- expected `proof_carry_data`,
- expected child input hash,
- expected replay block range.

**Step 2: Add fixture tests**

In `gaiko2`, assert:

- `GuestInput` validation passes,
- replay passes,
- returned input hash matches expected.

In `raiko2`, assert:

- serialized request matches the v2 fixture contract,
- conformance fixture round-trips through gaiko2.

**Step 3: Run fixture tests**

Run in `gaiko2`:

```bash
go test ./internal/prover -run 'TestGuestInputFixture'
```

Run in `raiko2`:

```bash
cargo test -p raiko2-prover --test gaiko2_shared_fixture -- --nocapture
```

Expected: PASS.

**Step 4: Commit**

```bash
git add testdata internal/prover/guestinput_fixture_test.go
git commit -m "test: add shasta guestinput soundness fixture"
```

Commit the `raiko2` fixture changes separately in the `raiko2` repository:

```bash
git add crates/prover/tests/fixtures crates/prover/tests/gaiko2_shared_fixture.rs
git commit -m "test: update gaiko2 guestinput fixture"
```

### Task 10: Full Verification

**Files:**
- Modify: none

**Step 1: Run gaiko2 tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 2: Run raiko2 targeted tests**

Run:

```bash
cargo test -p raiko2-prover --test gaiko2_adapter -- --nocapture
cargo test -p raiko2-prover --test remote_prover_conformance -- --ignored --nocapture
```

Expected: PASS, with a local gaiko2 service running for black-box conformance if required.

**Step 3: Check for replay-only schema references**

Run:

```bash
rg -n 'raiko2-shasta-request-v1|ShastaRequestSchemaV1|blocks.*proof_carry_data' internal docs testdata
```

Expected:

- v1 references remain only in historical docs, explicit compatibility code, or tests that assert rejection.
- production request docs describe v2 `guest_input`.

**Step 4: Commit final docs**

```bash
git add README.md docs testdata
git commit -m "docs: document gaiko2 guestinput proving"
```

### Task 11: Release Readiness Review

**Files:**
- Modify: none

**Step 1: Review the final diff**

Run:

```bash
git diff --stat main...
git diff main... -- internal/protocol internal/prover internal/api testdata docs
```

Check specifically:

- no production path signs v1 replay-only inputs,
- blob validation recomputes commitment from raw blobs,
- manifest binding runs before replay signing,
- aggregate validation remains contiguous and same-type,
- tests include negative cases for known soundness failures.

**Step 2: Document residual risks**

Add any unresolved items to the PR description:

- temporary local derivation implementation,
- compatibility flag lifetime,
- calldata source policy,
- missing mainnet-like fixture coverage.

**Step 3: Prepare PR**

```bash
git status --short
```

Expected: clean worktree after commits.
