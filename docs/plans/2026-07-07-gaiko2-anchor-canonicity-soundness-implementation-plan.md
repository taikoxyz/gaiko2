# Gaiko2 Anchor Canonicity Soundness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close two verified Shasta manifest-binding soundness gaps in `gaiko2` so a crafted `guest_input` can no longer obtain a TEE signature over a non-canonical L2 block hash.

**Architecture:** Two independent, local fixes in `internal/prover`. Fix A makes `decodeAnchorV4Checkpoint` reject any anchor calldata that is not exactly the canonical `selector + 96` ABI bytes (trailing bytes, dirty `uint48` padding). Fix B stops trusting the request-supplied checkpoint-store address: it derives the L2 SignalService predeploy address from the chain id (the same predeploy scheme already used for the TaikoL2/Anchor address) and rejects a disagreeing witness value. Both are test-driven; the checked-in mainnet replay fixture must stay green.

**Tech Stack:** Go 1.24, `go-ethereum` (`common`, `crypto`, `core/state`, `trie`, `triedb`), standard `go test`.

**Spec:** `docs/plans/2026-07-07-gaiko2-anchor-canonicity-soundness-design.md`

## Global Constraints

- Work on branch `fix/anchor-canonicity-soundness`.
- Run all tests from the repo root (`/Users/cai/taiko/gaiko2`).
- Do not change replay, aggregate hashing, TEE attestation, or the wire schema.
- The mainnet replay fixture (`testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`, exercised by `internal/prover/replay_fixture_test.go`) must keep passing.
- Pre-existing `internal/tee` permission-assertion failures (0644 vs 0600) are unrelated; ignore them.
- End every git commit message with the trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- `gofmt`/`goimports` after edits; add any missing imports the new code needs.

---

## File Structure

- `internal/prover/manifest_validate.go` — both fixes live here:
  - `decodeAnchorV4Checkpoint` (currently `:1046-1063`) — Fix A.
  - address-derivation constants (`:22-38`) and `shastaTaikoL2Address` (`:1031-1038`) — Fix B helper.
  - `decodeWitnessCheckpointStore` (`:1122-1131`) and `verifiedParentShastaCheckpoint` (`:1099-1120`) — Fix B pin.
- `internal/prover/manifest_validate_test.go` — new unit tests + one edit to the `newCheckpointStoreStateFixture` helper (`:2025-2029`).

---

## Task 1: Fix A — reject non-canonical anchorV4 calldata

**Files:**
- Modify: `internal/prover/manifest_validate.go:1046-1063` (`decodeAnchorV4Checkpoint`)
- Test: `internal/prover/manifest_validate_test.go` (new test function)

**Interfaces:**
- Consumes: existing `anchorV4CheckpointView` struct, `maxUint48` constant.
- Produces: unchanged signature `decodeAnchorV4Checkpoint(input []byte) (anchorV4CheckpointView, error)`; now errors on non-canonical length or padding.

- [ ] **Step 1: Write the failing test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestDecodeAnchorV4CheckpointRejectsNonCanonicalCalldata(t *testing.T) {
	selector := crypto.Keccak256([]byte("anchorV4((uint48,bytes32,bytes32))"))[:4]
	blockHash := common.HexToHash(testHash("ab"))
	stateRoot := common.HexToHash(testHash("cd"))
	canonical := func() []byte {
		out := append([]byte{}, selector...)
		var word [32]byte
		word[31] = 42 // uint48 blockNumber = 42, right-aligned with zero padding
		out = append(out, word[:]...)
		out = append(out, blockHash.Bytes()...)
		out = append(out, stateRoot.Bytes()...)
		return out
	}

	cp, err := decodeAnchorV4Checkpoint(canonical())
	if err != nil {
		t.Fatalf("canonical anchorV4 calldata should decode: %v", err)
	}
	if cp.blockNumber != 42 || cp.blockHash != blockHash || cp.stateRoot != stateRoot {
		t.Fatalf("unexpected decoded checkpoint: %+v", cp)
	}

	trailing := append(canonical(), 0xde, 0xad, 0xbe, 0xef)
	if _, err := decodeAnchorV4Checkpoint(trailing); err == nil ||
		!strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("expected trailing-calldata rejection, got %v", err)
	}

	dirty := canonical()
	dirty[4] = 0x01 // high byte of the blockNumber word
	if _, err := decodeAnchorV4Checkpoint(dirty); err == nil ||
		!strings.Contains(err.Error(), "padding") {
		t.Fatalf("expected non-canonical padding rejection, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/prover/ -run TestDecodeAnchorV4CheckpointRejectsNonCanonicalCalldata -v`
Expected: FAIL — trailing/dirty inputs currently decode without error.

- [ ] **Step 3: Implement the canonical-calldata guards**

Replace `decodeAnchorV4Checkpoint` (`manifest_validate.go:1046-1063`) with:

```go
func decodeAnchorV4Checkpoint(input []byte) (anchorV4CheckpointView, error) {
	selector := crypto.Keccak256([]byte("anchorV4((uint48,bytes32,bytes32))"))[:4]
	if len(input) < 4 || !bytes.Equal(input[:4], selector) {
		return anchorV4CheckpointView{}, fmt.Errorf("first transaction is not anchorV4")
	}
	// The canonical anchor transaction is exactly selector + 96 ABI bytes. Reject
	// trailing bytes: Solidity ignores them (fully static struct), so they leave
	// replay unchanged yet change the tx hash and therefore the signed block hash.
	// This binding runs before replay, so it must not rely on the on-chain uint48
	// cleanup revert to catch non-canonical encodings.
	if len(input) != 4+96 {
		return anchorV4CheckpointView{}, fmt.Errorf(
			"anchorV4 calldata length %d is not canonical (want %d)", len(input), 4+96)
	}
	// The uint48 blockNumber occupies the low 6 bytes of its 32-byte word; the
	// high 24 bytes must be zero for a canonical ABI encoding.
	if !bytes.Equal(input[4:4+24], make([]byte, 24)) {
		return anchorV4CheckpointView{}, fmt.Errorf("anchorV4 blockNumber has non-canonical padding")
	}
	blockNumber := binary.BigEndian.Uint64(input[4+24 : 4+32])
	if blockNumber > maxUint48 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchorV4 blockNumber exceeds uint48")
	}
	return anchorV4CheckpointView{
		blockNumber: blockNumber,
		blockHash:   common.BytesToHash(input[4+32 : 4+64]),
		stateRoot:   common.BytesToHash(input[4+64 : 4+96]),
	}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/prover/ -run TestDecodeAnchorV4CheckpointRejectsNonCanonicalCalldata -v`
Expected: PASS

- [ ] **Step 5: Run the anchor + fixture tests to confirm no regression**

Run: `go test ./internal/prover/ -run 'Anchor|ManifestBinding|Fixture' -v`
Expected: PASS (canonical anchors and the mainnet fixture still bind).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): reject non-canonical anchorV4 calldata

Require the anchor transaction calldata to be exactly selector+96 bytes with
canonical uint48 padding. Trailing bytes are ignored by Solidity (static struct)
so they do not change replay, but they change the tx hash and therefore the
signed block hash. Enforced at manifest-binding time, before replay.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Fix B (part 1) — derive the L2 SignalService predeploy address

**Files:**
- Modify: `internal/prover/manifest_validate.go:22-38` (const block) and `:1031-1038` (`shastaTaikoL2Address`)
- Test: `internal/prover/manifest_validate_test.go` (new test function)

**Interfaces:**
- Produces:
  - `shastaL2PredeployAddress(chainID uint64, suffix string) (common.Address, error)`
  - `shastaSignalServiceAddress(chainID uint64) (common.Address, error)` — suffix `"5"`
  - `shastaTaikoL2Address(chainID uint64) (common.Address, error)` — unchanged behavior, now a thin wrapper (suffix `"10001"`).
- Consumes: none new.

- [ ] **Step 1: Write the failing test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestShastaSignalServiceAddressDerivesPredeploy(t *testing.T) {
	got, err := shastaSignalServiceAddress(167000)
	if err != nil {
		t.Fatalf("derive signal service address: %v", err)
	}
	if want := common.HexToAddress("0x1670000000000000000000000000000000000005"); got != want {
		t.Fatalf("signal service address mismatch: got %s want %s", got.Hex(), want.Hex())
	}
	l2, err := shastaTaikoL2Address(167000)
	if err != nil {
		t.Fatalf("derive taikoL2 address: %v", err)
	}
	if want := common.HexToAddress("0x1670000000000000000000000000000000010001"); l2 != want {
		t.Fatalf("taikoL2 address regressed: got %s want %s", l2.Hex(), want.Hex())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/prover/ -run TestShastaSignalServiceAddressDerivesPredeploy -v`
Expected: FAIL — `shastaSignalServiceAddress` is undefined (compile error).

- [ ] **Step 3: Add the suffix constant**

In the `const (...)` block at `manifest_validate.go:22-38`, next to `shastaTaikoL2AddressSuffix = "10001"`, add:

```go
	shastaSignalServiceAddressSuffix = "5"
```

- [ ] **Step 4: Generalize the address derivation**

Replace `shastaTaikoL2Address` (`manifest_validate.go:1031-1038`) with:

```go
func shastaL2PredeployAddress(chainID uint64, suffix string) (common.Address, error) {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	padding := common.AddressLength*2 - len(prefix) - len(suffix)
	if padding < 0 {
		return common.Address{}, fmt.Errorf("chain_id %d is too long to derive L2 predeploy address", chainID)
	}
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + suffix), nil
}

func shastaTaikoL2Address(chainID uint64) (common.Address, error) {
	return shastaL2PredeployAddress(chainID, shastaTaikoL2AddressSuffix)
}

func shastaSignalServiceAddress(chainID uint64) (common.Address, error) {
	return shastaL2PredeployAddress(chainID, shastaSignalServiceAddressSuffix)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/prover/ -run TestShastaSignalServiceAddressDerivesPredeploy -v`
Expected: PASS

- [ ] **Step 6: Confirm existing TaikoL2-address users still pass**

Run: `go test ./internal/prover/ -run 'Recipient|ManifestBinding' -v`
Expected: PASS (the `shastaTaikoL2Address` wrapper is behavior-preserving).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): derive L2 SignalService predeploy address from chain id

Generalize the TaikoL2 predeploy derivation into shastaL2PredeployAddress and add
shastaSignalServiceAddress (suffix 5 -> 0x{chainid}...0005), the deterministic L2
contract that holds Shasta checkpoints at slot 254.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Fix B (part 2) — pin the checkpoint-store address in the linkage check

**Files:**
- Modify: `internal/prover/manifest_validate.go:1099-1131` (`verifiedParentShastaCheckpoint` call site + replace `decodeWitnessCheckpointStore`)
- Modify: `internal/prover/manifest_validate_test.go:2025-2029` (`newCheckpointStoreStateFixture` seeds at the derived address)
- Test: `internal/prover/manifest_validate_test.go` (new negative test)

**Interfaces:**
- Consumes: `shastaSignalServiceAddress` (Task 2), existing `optionalAddress`, `decodeJSONObject`, `readParentL2Storage`.
- Produces: `resolveShastaCheckpointStore(view *GuestInputView) (common.Address, error)` replacing `decodeWitnessCheckpointStore`. `verifiedParentShastaCheckpoint` keeps its signature but now reads from the derived store.

**Context — why the fixture edit is required:** `newCheckpointStoreStateFixture` currently seeds the checkpoint at an arbitrary account `0x1234…5678` (chainID 167001) and points the witness `checkpoint_store_contract` at it — i.e. it encodes the pre-fix behavior. After the fix, `verifiedParentShastaCheckpoint` reads from the derived address (`0x1670010000000000000000000000000000000005`), so the fixture must seed there. The real mainnet fixture already uses `…0005`, so nothing else regresses.

- [ ] **Step 1: Re-point the fixture at the derived store address (failing setup)**

In `newCheckpointStoreStateFixture` (`manifest_validate_test.go:2025-2029`), replace the hard-coded account line:

```go
	account := common.HexToAddress("0x1234567890AbcdEF1234567890aBcdef12345678")
```

with the derived address for the fixture chain id (`const chainID = uint64(167001)` is declared just above):

```go
	account, err := shastaSignalServiceAddress(chainID)
	if err != nil {
		t.Fatalf("derive checkpoint store address: %v", err)
	}
```

(The later `statedb, err := state.New(...)` still compiles: `err` is reused, `statedb` is new.)

- [ ] **Step 2: Write the failing negative test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestValidateAnchorL1LinkageRejectsRequestSelectedCheckpointStore(t *testing.T) {
	view, _, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	proposal := shastaProposalView{
		OriginBlockNumber: parentAnchor + 600,
		OriginBlockHash:   fixtureOriginHash(t, view),
	}
	// Repoint the request-controlled chain spec at an attacker-chosen store address.
	// The fix derives the store from chain id (167001 -> ...0005), so this must be
	// rejected even though the derived address still holds the matching checkpoint.
	attacker := common.HexToAddress("0x1234567890AbcdEF1234567890aBcdef12345678")
	view.Witnesses[0].ChainSpecRaw = json.RawMessage(
		fmt.Sprintf(`{"chain_id":167001,"checkpoint_store_contract":%q}`, attacker.Hex()))
	cp := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, parentAnchor)
	if err == nil || !strings.Contains(err.Error(), "does not match derived SignalService address") {
		t.Fatalf("expected request-selected checkpoint store rejection, got %v", err)
	}
}
```

- [ ] **Step 3: Run the new test to verify it fails**

Run: `go test ./internal/prover/ -run TestValidateAnchorL1LinkageRejectsRequestSelectedCheckpointStore -v`
Expected: FAIL — the current code trusts the request-selected address, so no such error is produced.

- [ ] **Step 4: Replace the store lookup with a derived-and-verified resolver**

In `verifiedParentShastaCheckpoint` (`manifest_validate.go:1099-1120`), change the first line:

```go
	store, err := decodeWitnessCheckpointStore(view)
```

to:

```go
	store, err := resolveShastaCheckpointStore(view)
```

Then replace the `decodeWitnessCheckpointStore` function (`manifest_validate.go:1122-1131`) with:

```go
// resolveShastaCheckpointStore returns the L2 SignalService address holding the
// parent checkpoint, derived from the trusted chain id rather than trusted from
// request-controlled witness.chain_spec. A witness that still carries a
// checkpoint_store_contract must agree with the derived address.
func resolveShastaCheckpointStore(view *GuestInputView) (common.Address, error) {
	derived, err := shastaSignalServiceAddress(view.GuestInputChainID)
	if err != nil {
		return common.Address{}, err
	}
	if len(view.Witnesses) == 0 {
		return derived, nil
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	supplied, err := optionalAddress(fields, "checkpoint_store_contract", "checkpointStoreContract")
	if err != nil {
		return common.Address{}, err
	}
	if supplied != nil && *supplied != derived {
		return common.Address{}, fmt.Errorf(
			"witness.chain_spec.checkpoint_store_contract %s does not match derived SignalService address %s",
			supplied.Hex(), derived.Hex())
	}
	return derived, nil
}
```

- [ ] **Step 5: Run the new negative test to verify it passes**

Run: `go test ./internal/prover/ -run TestValidateAnchorL1LinkageRejectsRequestSelectedCheckpointStore -v`
Expected: PASS

- [ ] **Step 6: Run the full checkpoint-store + linkage suite to confirm the positive paths still pass**

Run: `go test ./internal/prover/ -run 'CheckpointStore|AnchorL1Linkage|ReadParentL2Storage' -v`
Expected: PASS — including `TestValidateAnchorL1LinkageBypassMatchesParentCheckpoint`, `TestValidateAnchorL1LinkageForcedPrefixMatchesParentCheckpoint`, `TestReadParentL2StorageReturnsCheckpoint`, and `TestValidateAnchorL1LinkageRejectsUnboundWitnessParent` (now seeded at the derived address).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): pin Shasta checkpoint store to the derived SignalService

Derive the L2 checkpoint-store address from the trusted chain id instead of
reading it from request-controlled witness.chain_spec, and reject a supplied
checkpoint_store_contract that disagrees. Closes the stalled-anchor and
forced-inclusion path where an attacker-selected store address with seeded
storage could validate forged parent checkpoints.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Full regression gate

**Files:** none (verification only)

- [ ] **Step 1: Vet the package**

Run: `go vet ./internal/prover/...`
Expected: no output (clean).

- [ ] **Step 2: Run the whole prover test suite**

Run: `go test ./internal/prover/...`
Expected: PASS — includes the mainnet replay fixture (`replay_fixture_test.go`) and all manifest-binding tests.

- [ ] **Step 3: Run the full module (soundness sanity)**

Run: `go test ./...`
Expected: PASS for `cmd/gaiko2`, `internal/api`, `internal/protocol`, `internal/prover`. `internal/tee` may show the pre-existing 0644-vs-0600 permission-assertion failures — those are unrelated to this change and out of scope.

- [ ] **Step 4: No extra commit needed** unless `gofmt` reports changes; if so:

```bash
gofmt -w internal/prover/
git commit -am "chore(prover): gofmt

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Fix A (exact-canonical anchor calldata: length + padding) → Task 1. ✓
- Fix B (derive SignalService address via shared helper, suffix `"5"`) → Task 2. ✓
- Fix B (derive-and-reject-on-mismatch in the linkage read) → Task 3. ✓
- Tests from the spec — trailing byte, dirty padding, canonical passes (Task 1); derived-address assertion (Task 2); request-selected rejection + positive derived path (Task 3); mainnet fixture green (Tasks 1/3/4). ✓
- Non-goals (alethia-reth, other chain-spec fields, endpoint auth, replay/aggregate/schema untouched) — respected; no task touches them. ✓

**Placeholder scan:** No TBD/TODO; every code and command step is concrete. ✓

**Type consistency:** `shastaL2PredeployAddress`/`shastaSignalServiceAddress`/`shastaTaikoL2Address` return `(common.Address, error)` consistently; `resolveShastaCheckpointStore(view *GuestInputView) (common.Address, error)` matches the `verifiedParentShastaCheckpoint` call site; the new tests reference existing helpers (`testHash`, `newCheckpointStoreStateFixture`, `fixtureOriginHash`, `optionalAddress`) with their real signatures. ✓
