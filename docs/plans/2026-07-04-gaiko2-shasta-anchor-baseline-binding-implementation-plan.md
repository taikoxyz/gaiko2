# Shasta Anchor Baseline Binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bind the Shasta anchor-progression baseline to the authenticated parent `Anchor._blockState.anchorBlockNumber` (storage slot 256) read via the existing proof-backed `readParentL2Storage` at the trusted `shastaTaikoL2Address(chainID)`, instead of trusting the caller-supplied `taiko.prover_data.last_anchor_block_number`.

**Architecture:** Add a proof-backed reader (`verifiedParentAnchorBlockNumber`) that decodes slot 256 from the trusted TaikoL2 address in parent L2 state. Use its result as the authoritative baseline in `ValidateGuestInputManifestBindingWithContext`, and treat `prover_data.last_anchor_block_number` as an optional equality cross-check (raiko2 parity: `verified_parent_anchor_block_number`). No new trust subsystem — the anchor address already comes from a trusted, chain-id-derived resolver.

**Tech Stack:** Go, `github.com/ethereum/go-ethereum` (`common`, `core/state`, `trie`, `triedb`, `crypto`), module `github.com/taikoxyz/gaiko2`.

## Global Constraints

- Module: `github.com/taikoxyz/gaiko2`. No new third-party dependencies.
- Match raiko2 semantics (`crates/guest-common/src/lib.rs`): verified value is authoritative; `prover_data.last_anchor_block_number` is an optional equality cross-check; fail closed on read errors.
- Anchor storage: `Anchor._blockState.anchorBlockNumber` is a `uint48` packed into the least-significant 48 bits of the word at slot **256**.
- Trusted anchor address = `shastaTaikoL2Address(view.GuestInputChainID)` (production) / `testTaikoL2Address(chainID)` (tests) — never the witness `chain_spec.l2_contract`.
- Verified fact (spike against `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`): chain id `167000`, address `0x1670000000000000000000000000000000010001`, slot-256 word `0x…017b60a5`, decoded anchor `24862885`, which equals the fixture's `last_anchor_block_number`.
- TDD, frequent commits. Run `gofmt`/`go build ./...`/`go vet ./...` before each commit.
- All work on branch `fix/shasta-anchor-baseline-binding` (already created).

---

### Task 1: Anchor storage-word decoder + slot constant

**Files:**
- Modify: `internal/prover/manifest_validate.go` (add constant near `:42`, add function near `shastaCheckpointStorageSlots` at `:1076`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Produces: `const shastaAnchorBlockStateSlot uint64 = 256`; `anchorBlockNumberFromStorageWord(word common.Hash) uint64`

- [ ] **Step 1: Write the failing test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestAnchorBlockNumberFromStorageWord(t *testing.T) {
	// Real mainnet slot-256 word observed in the shared fixture.
	real := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000017b60a5")
	if got := anchorBlockNumberFromStorageWord(real); got != 24862885 {
		t.Fatalf("real word: got %d want 24862885", got)
	}
	// Full uint48 in the low 48 bits; higher-order bytes are other packed
	// struct fields and must be ignored.
	packed := common.HexToHash("0x" + strings.Repeat("ab", 26) + "ffffffffffff")
	if got := anchorBlockNumberFromStorageWord(packed); got != 281474976710655 {
		t.Fatalf("packed word: got %d want 281474976710655", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prover/ -run TestAnchorBlockNumberFromStorageWord -v`
Expected: FAIL — `undefined: anchorBlockNumberFromStorageWord`

- [ ] **Step 3: Add the constant**

In `internal/prover/manifest_validate.go`, below `const shastaSignalServiceCheckpointsSlot uint64 = 254` (`:40`):

```go
// shastaAnchorBlockStateSlot is the storage slot of Anchor._blockState on the
// TaikoL2 contract. anchorBlockNumber is a uint48 packed into the low 48 bits of
// the word at this slot. Matches raiko2's ANCHOR_BLOCK_STATE_SLOT.
const shastaAnchorBlockStateSlot uint64 = 256
```

- [ ] **Step 4: Add the decoder**

In `internal/prover/manifest_validate.go`, immediately after `shastaCheckpointStorageSlots` (ends `:1083`):

```go
// anchorBlockNumberFromStorageWord extracts Anchor._blockState.anchorBlockNumber,
// a uint48 packed into the least-significant 48 bits of the storage word.
func anchorBlockNumberFromStorageWord(word common.Hash) uint64 {
	return new(big.Int).SetBytes(word[26:32]).Uint64()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/prover/ -run TestAnchorBlockNumberFromStorageWord -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
go build ./...
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): decode Anchor._blockState.anchorBlockNumber from slot 256"
```

---

### Task 2: `verifiedParentAnchorBlockNumber` reader

**Files:**
- Modify: `internal/prover/manifest_validate.go` (add function near `verifiedParentShastaCheckpoint` at `:1085`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Consumes: `shastaTaikoL2Address(chainID uint64) (common.Address, error)` (`:1017`), `readParentL2Storage(view *GuestInputView, account common.Address, slot common.Hash) (common.Hash, error)` (`internal/prover/l2_state.go:14`), `anchorBlockNumberFromStorageWord` (Task 1)
- Produces: `verifiedParentAnchorBlockNumber(view *GuestInputView) (uint64, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/prover/manifest_validate_test.go` (same package as `loadSharedShastaFixture`):

```go
func TestVerifiedParentAnchorBlockNumberReadsSharedFixture(t *testing.T) {
	req := loadSharedShastaFixture(t)
	view, err := DecodeGuestInput(*req.Payload.GuestInput)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}
	got, err := verifiedParentAnchorBlockNumber(view)
	if err != nil {
		t.Fatalf("verified parent anchor: %v", err)
	}
	if got != 24862885 {
		t.Fatalf("verified parent anchor: got %d want 24862885", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prover/ -run TestVerifiedParentAnchorBlockNumberReadsSharedFixture -v`
Expected: FAIL — `undefined: verifiedParentAnchorBlockNumber`

- [ ] **Step 3: Implement the reader**

In `internal/prover/manifest_validate.go`, immediately before `verifiedParentShastaCheckpoint` (`:1085`):

```go
// verifiedParentAnchorBlockNumber reads the parent block's
// Anchor._blockState.anchorBlockNumber from proof-backed L2 state at the trusted
// TaikoL2 address derived from the chain id. This is the authenticated baseline
// for Shasta anchor progression; a caller-supplied
// taiko.prover_data.last_anchor_block_number must not be trusted in its place.
func verifiedParentAnchorBlockNumber(view *GuestInputView) (uint64, error) {
	l2Address, err := shastaTaikoL2Address(view.GuestInputChainID)
	if err != nil {
		return 0, fmt.Errorf("derive TaikoL2 address for parent anchor state: %w", err)
	}
	slot := common.BigToHash(new(big.Int).SetUint64(shastaAnchorBlockStateSlot))
	word, err := readParentL2Storage(view, l2Address, slot)
	if err != nil {
		return 0, fmt.Errorf("read parent Anchor._blockState.anchorBlockNumber: %w", err)
	}
	return anchorBlockNumberFromStorageWord(word), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/prover/ -run TestVerifiedParentAnchorBlockNumberReadsSharedFixture -v`
Expected: PASS (proves the read works end-to-end against real mainnet parent state)

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
go build ./...
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): read verified parent anchor baseline from L2 state"
```

Note: `verifiedParentAnchorBlockNumber` is not yet called by the binding flow (wired in Task 4). An `unusedfunc` linter *info* hint is expected until then; it is not a build error.

---

### Task 3: Test-fixture anchor-seeding infrastructure (no behavior change)

Seeds the anchor contract's slot 256 into the shared manifest-binding state helpers so that, once Task 4 wires the read into the flow, every existing test still has a bound baseline. This task changes **tests only**; production behavior is unchanged, so the whole suite must stay green.

**Files:**
- Modify: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Consumes: `testTaikoL2Address(chainID uint64) common.Address` (`:698`), `collectTrieNodes(t, tr *trie.StateTrie) []string` (`:2129`), `shastaAnchorBlockStateSlot` (Task 1)
- Produces (test helpers): `manifestTestChainID`, `manifestDefaultParentAnchorBlockNumber`, `manifestUint64Ptr(uint64) *uint64`, `witnessStateNodesWithBalancesAndAnchor(t, balances, anchorBaseline) ([]string, common.Hash)`, `collectStateAndStorageNodes(t, tdb, root, storageAccount) []string`; new fixture field `manifestBindingFixture.lastAnchorBlockNumber *uint64`

- [ ] **Step 1: Add test-level constants and pointer helper**

In `internal/prover/manifest_validate_test.go`, near the top-level test helpers (e.g. above `witnessStateNodesWithBalance` at `:1566`):

```go
const (
	manifestTestChainID                    = uint64(167001)
	manifestDefaultParentAnchorBlockNumber = uint64(899)
)

func manifestUint64Ptr(v uint64) *uint64 { return &v }
```

- [ ] **Step 2: Add the combined account+storage node collector**

In `internal/prover/manifest_validate_test.go`, near `collectTrieNodes` (`:2129`):

```go
// collectStateAndStorageNodes returns the account-trie nodes for `root` plus the
// storage-trie nodes for `storageAccount`, so a proof-backed slot read against
// storageAccount resolves from the witness state set.
func collectStateAndStorageNodes(
	t *testing.T,
	tdb *triedb.Database,
	root common.Hash,
	storageAccount common.Address,
) []string {
	t.Helper()
	accountTrie, err := trie.NewStateTrie(trie.StateTrieID(root), tdb)
	if err != nil {
		t.Fatalf("open account trie: %v", err)
	}
	nodes := collectTrieNodes(t, accountTrie)
	account, err := accountTrie.GetAccount(storageAccount)
	if err != nil || account == nil {
		t.Fatalf("resolve storage account %s: %v", storageAccount.Hex(), err)
	}
	storageTrie, err := trie.NewStateTrie(
		trie.StorageTrieID(root, crypto.Keccak256Hash(storageAccount.Bytes()), account.Root),
		tdb,
	)
	if err != nil {
		t.Fatalf("open storage trie: %v", err)
	}
	return append(nodes, collectTrieNodes(t, storageTrie)...)
}
```

- [ ] **Step 3: Add the anchor-seeding state builder and route the balances helper through it**

In `internal/prover/manifest_validate_test.go`, replace the body of `witnessStateNodesWithBalances` (`:1571-1610`) with a delegating wrapper and a new anchor-aware builder:

```go
func witnessStateNodesWithBalances(t *testing.T, balances map[common.Address]*big.Int) ([]string, common.Hash) {
	t.Helper()
	return witnessStateNodesWithBalancesAndAnchor(t, balances, manifestDefaultParentAnchorBlockNumber)
}

// witnessStateNodesWithBalancesAndAnchor builds a parent state trie holding the
// given balances plus the TaikoL2 anchor contract's _blockState.anchorBlockNumber
// (slot shastaAnchorBlockStateSlot = anchorBaseline). It returns the combined
// account- and storage-trie nodes (for the witness state set) and the state root.
func witnessStateNodesWithBalancesAndAnchor(
	t *testing.T,
	balances map[common.Address]*big.Int,
	anchorBaseline uint64,
) ([]string, common.Hash) {
	t.Helper()

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("open test state: %v", err)
	}
	for address, balance := range balances {
		statedb.AddBalance(address, uint256.MustFromBig(balance), 0)
	}
	anchorAddress := testTaikoL2Address(manifestTestChainID)
	statedb.SetNonce(anchorAddress, 1, tracing.NonceChangeUnspecified)
	statedb.SetState(
		anchorAddress,
		common.BigToHash(new(big.Int).SetUint64(shastaAnchorBlockStateSlot)),
		common.BigToHash(new(big.Int).SetUint64(anchorBaseline)),
	)

	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit test state: %v", err)
	}
	return collectStateAndStorageNodes(t, tdb, root, anchorAddress), root
}
```

`witnessStateNodesWithBalance` (`:1566`) is unchanged; it delegates to `witnessStateNodesWithBalances` and therefore now also seeds the anchor.

- [ ] **Step 4: Seed the anchor in the balance+code builder**

In `internal/prover/manifest_validate_test.go`, replace the body of `witnessStateNodesWithBalanceAndCode` (`:1612-1656`) with:

```go
func witnessStateNodesWithBalanceAndCode(
	t *testing.T,
	balanceAddress common.Address,
	balance *big.Int,
	codeAddress common.Address,
	code []byte,
) ([]string, []string, common.Hash) {
	t.Helper()

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("open test state: %v", err)
	}
	statedb.AddBalance(balanceAddress, uint256.MustFromBig(balance), 0)
	statedb.SetCode(codeAddress, code, tracing.CodeChangeUnspecified)
	anchorAddress := testTaikoL2Address(manifestTestChainID)
	statedb.SetNonce(anchorAddress, 1, tracing.NonceChangeUnspecified)
	statedb.SetState(
		anchorAddress,
		common.BigToHash(new(big.Int).SetUint64(shastaAnchorBlockStateSlot)),
		common.BigToHash(new(big.Int).SetUint64(manifestDefaultParentAnchorBlockNumber)),
	)

	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit test state: %v", err)
	}
	return collectStateAndStorageNodes(t, tdb, root, anchorAddress),
		[]string{"0x" + hex.EncodeToString(code)},
		root
}
```

- [ ] **Step 5: Add the `lastAnchorBlockNumber` fixture field and default**

In `internal/prover/manifest_validate_test.go`, add a field to `manifestBindingFixture` (struct at `:713`), after `anchorBlockNumber uint64` (`:740`):

```go
	lastAnchorBlockNumber       *uint64
```

In `newManifestBindingFixture` (`:762`), change the local chain-id line (`:765`) from `chainID := uint64(167001)` to:

```go
	chainID := manifestTestChainID
```

and add to the struct literal (`:775`), after `anchorBlockNumber: 900,` (`:832`):

```go
		lastAnchorBlockNumber: manifestUint64Ptr(manifestDefaultParentAnchorBlockNumber),
```

- [ ] **Step 6: Parameterize `last_anchor_block_number` in `view()`**

In `internal/prover/manifest_validate_test.go`, `view()` (`:904`), replace the `Taiko:` block (`:943-952`) so `prover_data` is built from the fixture field. Insert before the `input := protocol.ShastaGuestInput{` line (`:934`):

```go
	proverData := fmt.Sprintf(`"actual_prover": %q`, testAddress("77"))
	if f.lastAnchorBlockNumber != nil {
		proverData += fmt.Sprintf(`, "last_anchor_block_number": %d`, *f.lastAnchorBlockNumber)
	}
```

and replace the `Taiko:` field (`:943-952`) with:

```go
		Taiko: mustRawMessage(t, fmt.Sprintf(`{
			"chain_spec": {"chain_id": %d},
			"proposal_id": %d,
			"proposal_event": {"proposal": %s},
			"prover_data": {%s},
			"data_sources": [%s]%s
		}`, f.chainID, f.proposalID, proposalJSON, proverData, dataSourceJSON, f.l1HeadersJSON(t))),
```

- [ ] **Step 7: Run the full prover suite — must stay green**

Run: `go test ./internal/prover/`
Expected: PASS. Production still reads the field (unchanged); the extra anchor account is inert. If a test fails to compile or resolve state, fix the helper wiring before proceeding.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/prover/manifest_validate_test.go
go build ./... && go vet ./internal/prover/
git add internal/prover/manifest_validate_test.go
git commit -m "test(prover): seed parent anchor slot 256 in manifest-binding fixtures"
```

---

### Task 4: Bind the baseline to verified parent anchor state

Wires `verifiedParentAnchorBlockNumber` into the binding flow as the authoritative baseline and cross-checks the optional prover field. Driven by the forged-baseline regression test.

**Files:**
- Modify: `internal/prover/manifest_validate.go` (`decodeGuestInputLastAnchorBlockNumber` at `:1151`; binding flow at `:110-113`)
- Modify: `internal/prover/manifest_validate_test.go` (add exploit test; update partial-witness test at `:456`)

**Interfaces:**
- Consumes: `verifiedParentAnchorBlockNumber` (Task 2), `witnessStateNodesWithBalancesAndAnchor` + `lastAnchorBlockNumber` field (Task 3)
- Produces: `decodeGuestInputLastAnchorBlockNumber(raw json.RawMessage) (*uint64, error)` (signature change)

- [ ] **Step 1: Write the failing exploit-regression test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestValidateManifestBindingRejectsForgedLastAnchorBaseline(t *testing.T) {
	// PoC: the real parent Anchor baseline is 900, but the request forges
	// prover_data.last_anchor_block_number to 899 so a non-advancing normal
	// source (anchor 900) looks like an advance. The verified baseline must
	// override the forged field and reject.
	fixture := newManifestBindingFixture(t)
	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalancesAndAnchor(
		t,
		map[common.Address]*big.Int{signer: new(big.Int).SetUint64(1_000_000_000_000_000_000)},
		900,
	)
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes
	fixture.lastAnchorBlockNumber = manifestUint64Ptr(899)

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected forged last_anchor_block_number to be rejected")
	}
	if !strings.Contains(err.Error(), "last_anchor_block_number mismatch") {
		t.Fatalf("expected baseline mismatch error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prover/ -run TestValidateManifestBindingRejectsForgedLastAnchorBaseline -v`
Expected: FAIL — current code trusts the forged field (`899`), so binding succeeds and `err == nil`.

- [ ] **Step 3: Change `decodeGuestInputLastAnchorBlockNumber` to return `*uint64`**

In `internal/prover/manifest_validate.go`, replace the function (`:1151-1172`) with:

```go
func decodeGuestInputLastAnchorBlockNumber(raw json.RawMessage) (*uint64, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal taiko: %w", err)
	}
	proverDataRaw, ok := lookupField(fields, "prover_data", "proverData")
	if !ok || isEmptyOrNullRawMessage(proverDataRaw) {
		return nil, nil
	}
	proverData, err := decodeJSONObject(proverDataRaw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal taiko.prover_data: %w", err)
	}
	lastAnchor, err := optionalUint64Ptr(proverData, "last_anchor_block_number", "lastAnchorBlockNumber")
	if err != nil {
		return nil, fmt.Errorf("parse taiko.prover_data.last_anchor_block_number: %w", err)
	}
	return lastAnchor, nil
}
```

- [ ] **Step 4: Wire the verified baseline + cross-check into the binding flow**

In `internal/prover/manifest_validate.go`, replace the block at `:110-113`:

```go
	lastAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
	if err != nil {
		return err
	}
```

with:

```go
	lastAnchor, err := verifiedParentAnchorBlockNumber(view)
	if err != nil {
		return err
	}
	hostAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
	if err != nil {
		return err
	}
	if hostAnchor != nil && *hostAnchor != lastAnchor {
		return fmt.Errorf(
			"prover_data.last_anchor_block_number mismatch: expected %d (parent Anchor state), got %d",
			lastAnchor, *hostAnchor)
	}
```

The downstream `parent := shastaManifestParentContext{... AnchorBlockNumber: lastAnchor}` (`:114-118`) is unchanged and now receives the authenticated `uint64`.

- [ ] **Step 5: Run the exploit test to verify it passes**

Run: `go test ./internal/prover/ -run TestValidateManifestBindingRejectsForgedLastAnchorBaseline -v`
Expected: PASS — `err` contains `last_anchor_block_number mismatch`.

- [ ] **Step 6: Fix the partial-witness test (failure site moved to the anchor read)**

The upfront verified-anchor read now fails closed before filtering when the witness is truncated. In `internal/prover/manifest_validate_test.go`, update `TestValidateManifestBindingRejectsPartialWitnessStateDuringFiltering` (`:456`): rename it and update the assertion.

Rename the function:

```go
func TestValidateManifestBindingRejectsPartialWitnessStateForAnchorBaseline(t *testing.T) {
```

Replace the assertion (`:471-473`):

```go
	if !strings.Contains(err.Error(), "parent Anchor._blockState") {
		t.Fatalf("expected fail-closed parent anchor read error, got %v", err)
	}
```

- [ ] **Step 7: Run the full prover suite**

Run: `go test ./internal/prover/`
Expected: PASS. Coverage of every full-binding call site:
- Real-fixture tests (`TestSharedShastaFixture*`, `TestValidateManifestBindingAcceptsRealFixtureL1Linkage`) read `24862885` from real parent state and match the fixture field.
- Synthetic fixture tests (incl. `validate_test.go` `TestValidateRequestAcceptsGuestInputV1` and the manifest-mismatch/metadata tests) read the seeded `899`, matching the default field; the timestamp-mismatch tests still reach their `manifest block 0` error *after* the passing anchor read.
- `validate_test.go` tests that reject before binding (missing `guest_input`, unsupported schema) are unaffected.
- Only `TestValidateManifestBindingRejectsPartialWitnessStateForAnchorBaseline` (step 6) changes, because its truncated witness now fails at the anchor read.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
go build ./... && go vet ./internal/prover/
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): bind shasta anchor baseline to verified parent anchor state"
```

---

### Task 5: Cross-check coverage (absent + matching field) and final gate

**Files:**
- Modify: `internal/prover/manifest_validate_test.go` (add two tests)

**Interfaces:**
- Consumes: `manifestBindingFixture.lastAnchorBlockNumber`, `manifestUint64Ptr`, `manifestDefaultParentAnchorBlockNumber` (Task 3)

- [ ] **Step 1: Write the absent-field test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestValidateManifestBindingUsesVerifiedAnchorWhenFieldAbsent(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.lastAnchorBlockNumber = nil // omit prover_data.last_anchor_block_number
	// Default seeded anchor baseline is 899; the derived source advances to 900.
	if err := ValidateGuestInputManifestBinding(fixture.view(t)); err != nil {
		t.Fatalf("expected binding to succeed using the verified anchor, got %v", err)
	}
}
```

- [ ] **Step 2: Write the present-and-equal test**

Add to `internal/prover/manifest_validate_test.go`:

```go
func TestValidateManifestBindingAcceptsMatchingLastAnchorBaseline(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.lastAnchorBlockNumber = manifestUint64Ptr(manifestDefaultParentAnchorBlockNumber) // 899 == seeded slot 256
	if err := ValidateGuestInputManifestBinding(fixture.view(t)); err != nil {
		t.Fatalf("expected binding to succeed when field matches verified anchor, got %v", err)
	}
}
```

- [ ] **Step 3: Run the two new tests**

Run: `go test ./internal/prover/ -run 'TestValidateManifestBinding(UsesVerifiedAnchorWhenFieldAbsent|AcceptsMatchingLastAnchorBaseline)' -v`
Expected: PASS for both.

- [ ] **Step 4: Full build, vet, and test gate**

Run:
```bash
gofmt -l internal/prover/
go build ./...
go vet ./...
go test ./...
```
Expected: `gofmt -l` prints nothing; build/vet clean; all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate_test.go
git commit -m "test(prover): cover absent and matching last_anchor_block_number cross-check"
```

---

## Self-Review

**1. Spec coverage:**
- Derive baseline from slot 256 at trusted `shastaTaikoL2Address` → Task 2 (reader), Task 4 (wired in).
- `uint48` low-48-bit decode → Task 1.
- Optional equality cross-check (raiko2 parity) → Task 4 step 4; covered by Tasks 4 (mismatch), 5 (absent, equal).
- Fail-closed on read error → inherited from `readParentL2Storage`; asserted by Task 4 step 6 (partial witness).
- Feasibility on real wire → Task 2 test + `TestSharedShastaFixture*` staying green in Task 4.
- Shared-fixture seeding prerequisite → Task 3.
- Scope non-goals (`checkpoint_store_contract`, general `validate_known_chain_spec`) → intentionally untouched.

**2. Placeholder scan:** No TBD/TODO; every code step shows complete code and exact commands.

**3. Type consistency:** `decodeGuestInputLastAnchorBlockNumber` returns `*uint64` (Task 4) and is consumed as `hostAnchor != nil` / `*hostAnchor`. `verifiedParentAnchorBlockNumber` returns `(uint64, error)`; `lastAnchor` stays `uint64` into `shastaManifestParentContext.AnchorBlockNumber uint64`. Helper names (`witnessStateNodesWithBalancesAndAnchor`, `collectStateAndStorageNodes`, `manifestUint64Ptr`, `manifestTestChainID`, `manifestDefaultParentAnchorBlockNumber`) are used consistently across Tasks 3–5.
