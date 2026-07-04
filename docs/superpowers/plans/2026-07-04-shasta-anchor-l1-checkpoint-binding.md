# Shasta Anchor L1 Checkpoint Binding + Envelope — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make gaiko2 reject any Shasta proof request whose anchor transaction is malformed (Fix 2) or whose anchorV4 L1 checkpoint `blockHash`/`stateRoot` is not bound to the real L1 chain (Fix 1), matching raiko2's checks.

**Architecture:** Two independent additions to the manifest-binding validator in `internal/prover`. Fix 2 tightens the per-anchor-tx envelope checks in `validateManifestAnchorTransaction`. Fix 1 adds a proposal-level `validateAnchorL1Linkage` pass that (a) matches each anchor checkpoint's `(blockHash, stateRoot)` against the `l1_ancestor_headers` already carried in the guest input, anchored to `proposal.OriginBlockHash`, and (b) for forced-inclusion / stalled-anchor cases, reads the parent checkpoint from the L2 CheckpointStore contract storage via the witness + `proposal_state_nodes`.

**Tech Stack:** Go 1.24, `github.com/taikoxyz/taiko-geth` fork of go-ethereum (`core/types`, `core/state`, `triedb`, `crypto`, `rlp`), standard Go testing.

## Global Constraints

- Reference implementation is raiko2 (`/Users/davidcai/taiko/raiko2`). Every check must match its semantics; when in doubt, replicate raiko2 exactly.
- GoldenTouch account: `0x0000777735367b36bC9B61C50022d9D0700dB4Ec` (public key — its signature authenticates nothing; only the tx contents are trusted).
- Anchor gas limit: `1_000_000` (existing const `shastaAnchorGasLimit`).
- CheckpointStore checkpoints mapping base slot: **`254`** (raiko2 `SHASTA_SIGNAL_SERVICE_CHECKPOINTS_SLOT`). Derivation is a **single** mapping `keccak256(blockNumber_be32 || 254_be32)` for `blockHash`, `+1` for `stateRoot`. Do NOT use taiko-mono's newer nested `[VERSION][blockNumber]` layout — gaiko2 targets the same deployed contract raiko2 does.
- All validation is fail-closed: any mismatch, missing field, or unsupported shape returns an error; never accept unbound data.
- Anchor `maxAnchorOffset`: 512 for chain `167000` (`shastaMaxAnchorOffsetMainnet`), else 128 (`shastaMaxAnchorOffset`) — existing `anchorMaxOffsetForChain`.

---

## Phase 1 — Fix 2: anchor transaction envelope

### Task 1: Tighten anchor tx envelope validation

Recreates the lost working-tree change. A captured patch for the non-test portion exists at `/private/tmp/claude-501/-Users-davidcai-taiko-gaiko2/00947c3a-ead9-4c02-84b7-e1a66aefefaa/scratchpad/fix2-anchor-envelope-wip.patch` (`git apply` it as a shortcut, then verify against the code below). The test portion must be written fresh.

**Files:**
- Modify: `internal/prover/manifest_validate.go` (`validateManifestAnchorTransaction` ~line 932; replace `decodeWitnessL2Contract` with `shastaTaikoL2Address`; add consts/var near line 34-40)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Produces: `func shastaTaikoL2Address(chainID uint64) (common.Address, error)`; package var `shastaGoldenTouchAccount common.Address`; const `shastaTaikoL2AddressSuffix = "10001"`.
- `validateManifestAnchorTransaction` keeps signature `func(view *GuestInputView, tx *types.Transaction, header *types.Header, expected shastaManifestBlock) error` for now (Task 3 changes its return type).

- [ ] **Step 1: Write failing tests for the new envelope rejections**

Add to `internal/prover/manifest_validate_test.go`. The fixture helper `newManifestBindingFixture` must gain `anchorGas uint64` and `anchorPrivateKeyHex string` fields, default `anchorGas: shastaAnchorGasLimit`, `anchorPrivateKeyHex: nativeProofPrivateKey`, and `l2Contract`/`anchorTo` default to `testTaikoL2Address(chainID)`. The fixture's `anchorTxJSON` must sign a canonical EIP-1559 tx with `anchorPrivateKeyHex` and emit the real signature (replacing any hardcoded `"r":"0x1"` stub):

```go
func (f *manifestBindingFixture) anchorTxJSON(t *testing.T) json.RawMessage {
	t.Helper()
	input := anchorInput(t, f.anchorBlockNumber, common.HexToHash(testHash("61")), common.HexToHash(testHash("62")))
	key, err := crypto.HexToECDSA(f.anchorPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse anchor tx key: %v", err)
	}
	cfg, err := chainConfigFor(f.chainID)
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: new(big.Int).SetUint64(f.chainID), Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: new(big.Int).SetUint64(f.blockBaseFee), Gas: f.anchorGas, To: &f.anchorTo,
		Value: big.NewInt(0), Data: input,
	})
	signed, err := types.SignTx(tx, types.MakeSigner(cfg, new(big.Int).SetUint64(f.blockNumber), f.blockTimestamp), key)
	if err != nil {
		t.Fatalf("sign anchor tx: %v", err)
	}
	v, r, s := signed.RawSignatureValues()
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x%s", "s": "0x%s", "yParity": "0x%s"},
		"transaction": {"Eip1559": {"chain_id": "0x%x", "nonce": "0x0",
			"max_priority_fee_per_gas": "0x0", "max_fee_per_gas": "0x%x", "gas": "0x%x",
			"to": %q, "value": "0x0", "input": %q, "access_list": []}}}`,
		r.Text(16), s.Text(16), v.Text(16), f.chainID, f.blockBaseFee, f.anchorGas,
		f.anchorTo.Hex(), "0x"+hex.EncodeToString(input)))
}
```

The three rejection tests:

```go
func TestValidateManifestBindingRejectsNonCanonicalAnchorGas(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.anchorGas = shastaAnchorGasLimit + 1
	view := fixture.view(t)
	err := ValidateGuestInputManifestBinding(view)
	if err == nil || !strings.Contains(err.Error(), "anchor transaction gas limit mismatch") {
		t.Fatalf("expected anchor gas rejection, got %v", err)
	}
}

func TestValidateManifestBindingRejectsNonGoldenTouchAnchorSender(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.anchorPrivateKeyHex = manifestTestTxPrivateKeyHex
	view := fixture.view(t)
	block, _, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	err = validateManifestAnchorTransaction(view, block.Transactions()[0], block.Header(), shastaManifestBlock{
		AnchorBlockNumber: fixture.anchorBlockNumber,
	})
	if err == nil || !strings.Contains(err.Error(), "anchor transaction sender mismatch") {
		t.Fatalf("expected anchor sender rejection, got %v", err)
	}
}

func TestValidateManifestBindingRejectsRequestControlledAnchorRecipient(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	attacker := common.HexToAddress(testAddress("44"))
	fixture.l2Contract = attacker
	fixture.anchorTo = attacker
	view := fixture.view(t)
	err := ValidateGuestInputManifestBinding(view)
	if err == nil || !strings.Contains(err.Error(), "anchor transaction recipient mismatch") {
		t.Fatalf("expected canonical anchor recipient rejection, got %v", err)
	}
}

func testTaikoL2Address(chainID uint64) common.Address {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	const suffix = "10001"
	padding := common.AddressLength*2 - len(prefix) - len(suffix)
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + suffix)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/prover/ -run 'TestValidateManifestBindingRejects(NonCanonicalAnchorGas|NonGoldenTouchAnchorSender|RequestControlledAnchorRecipient)' -count=1`
Expected: FAIL (compile error for missing fixture fields / `shastaGoldenTouchAccount`, or assertion failures).

- [ ] **Step 3: Add the consts/var and envelope checks in `manifest_validate.go`**

Near the const block (~line 38) add `shastaTaikoL2AddressSuffix = "10001"`, and after it:
```go
var shastaGoldenTouchAccount = common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec")
```
In `validateManifestAnchorTransaction`, at the top add the type check and switch the recipient to canonical derivation:
```go
	if tx.Type() != types.DynamicFeeTxType {
		return fmt.Errorf("anchor transaction type mismatch: expected %d got %d", types.DynamicFeeTxType, tx.Type())
	}
	expectedRecipient, err := shastaTaikoL2Address(view.GuestInputChainID)
	if err != nil {
		return err
	}
```
After the `chain_id` check add:
```go
	if tx.Value().Sign() != 0 {
		return fmt.Errorf("anchor transaction value mismatch: expected 0 got %s", tx.Value())
	}
	if tx.Gas() != shastaAnchorGasLimit {
		return fmt.Errorf("anchor transaction gas limit mismatch: expected %d got %d", shastaAnchorGasLimit, tx.Gas())
	}
```
After the `header.BaseFee == nil` check add:
```go
	if header.Number == nil {
		return fmt.Errorf("block header is missing number for anchor transaction validation")
	}
```
After the access-list check add sender recovery:
```go
	config, err := chainConfigFor(view.GuestInputChainID)
	if err != nil {
		return err
	}
	sender, err := types.Sender(types.MakeSigner(config, header.Number, header.Time), tx)
	if err != nil {
		return fmt.Errorf("recover anchor transaction sender: %w", err)
	}
	if sender != shastaGoldenTouchAccount {
		return fmt.Errorf("anchor transaction sender mismatch: expected %s got %s",
			shastaGoldenTouchAccount.Hex(), sender.Hex())
	}
```
Replace `decodeWitnessL2Contract` with:
```go
func shastaTaikoL2Address(chainID uint64) (common.Address, error) {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	padding := common.AddressLength*2 - len(prefix) - len(shastaTaikoL2AddressSuffix)
	if padding < 0 {
		return common.Address{}, fmt.Errorf("chain_id %d is too long to derive TaikoL2 address", chainID)
	}
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + shastaTaikoL2AddressSuffix), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/prover/ -run TestValidateManifestBinding -count=1`
Expected: PASS (all manifest-binding tests, including the three new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): validate shasta anchor transaction envelope

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase 2 — Fix 1 core: normal-path L1 checkpoint linkage

### Task 2: Decode `taiko.l1_header` and `taiko.l1_ancestor_headers`

**Files:**
- Modify: `internal/prover/manifest_validate.go` (new helpers)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Produces: `func decodeGuestInputL1Headers(raw json.RawMessage) (*types.Header, []*types.Header, error)` returning `(l1Header, l1AncestorHeaders, err)`. Uses existing `decodeHeader` (handles the camelCase JSON in the fixture). `raw` is `view.TaikoRaw`.

- [ ] **Step 1: Write failing test**

```go
func TestDecodeGuestInputL1HeadersReadsOriginAndAncestors(t *testing.T) {
	raw := mustRawMessage(t, `{"l1_header":{`+minimalHeaderJSON(100)+`},
		"l1_ancestor_headers":[{`+minimalHeaderJSON(99)+`},{`+minimalHeaderJSON(100)+`}]}`)
	origin, ancestors, err := decodeGuestInputL1Headers(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if origin.Number.Uint64() != 100 {
		t.Fatalf("origin number: got %d", origin.Number.Uint64())
	}
	if len(ancestors) != 2 || ancestors[1].Number.Uint64() != 100 {
		t.Fatalf("ancestors: got %d entries", len(ancestors))
	}
}
```
Add a `minimalHeaderJSON(number uint64) string` helper emitting every field `decodeHeader` requires (`number, gas_limit, gas_used, timestamp, difficulty, logs_bloom, extra_data, parent_hash, ommers_hash, state_root, transactions_root, receipts_root, beneficiary, mix_hash, nonce`) with valid-length zero values, mirroring the existing header JSON used in `blockJSON`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run TestDecodeGuestInputL1Headers -count=1`
Expected: FAIL with "decodeGuestInputL1Headers not defined".

- [ ] **Step 3: Implement the decoder**

```go
func decodeGuestInputL1Headers(raw json.RawMessage) (*types.Header, []*types.Header, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("unmarshal taiko: %w", err)
	}
	l1HeaderRaw, ok := lookupField(fields, "l1_header", "l1Header")
	if !ok || isEmptyOrNullRawMessage(l1HeaderRaw) {
		return nil, nil, fmt.Errorf("missing taiko.l1_header")
	}
	l1Header, err := decodeHeader(l1HeaderRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode taiko.l1_header: %w", err)
	}
	ancestorsRaw, ok := lookupField(fields, "l1_ancestor_headers", "l1AncestorHeaders")
	if !ok || isEmptyOrNullRawMessage(ancestorsRaw) {
		return l1Header, nil, nil
	}
	var rawList []json.RawMessage
	if err := json.Unmarshal(ancestorsRaw, &rawList); err != nil {
		return nil, nil, fmt.Errorf("unmarshal taiko.l1_ancestor_headers: %w", err)
	}
	ancestors := make([]*types.Header, len(rawList))
	for i, r := range rawList {
		h, err := decodeHeader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("decode taiko.l1_ancestor_headers[%d]: %w", i, err)
		}
		ancestors[i] = h
	}
	return l1Header, ancestors, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/prover/ -run TestDecodeGuestInputL1Headers -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): decode shasta l1 anchor headers from guest input

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 3: Collect anchor checkpoints across derived blocks

**Files:**
- Modify: `internal/prover/manifest_validate.go` (`validateManifestAnchorTransaction`, `validateManifestBlockBinding`, `ValidateGuestInputManifestBindingWithContext`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Changes `validateManifestAnchorTransaction` return type to `(anchorV4CheckpointView, error)` (returns the decoded checkpoint it already parses).
- Changes `validateManifestBlockBinding` to return `(anchorV4CheckpointView, error)`.
- `ValidateGuestInputManifestBindingWithContext` collects `[]anchorV4CheckpointView` (one per derived block, in order) for use by Task 4.

- [ ] **Step 1: Write failing test**

```go
func TestValidateManifestBindingCollectsAnchorCheckpoints(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)
	block, _, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cp, err := validateManifestAnchorTransaction(view, block.Transactions()[0], block.Header(),
		shastaManifestBlock{AnchorBlockNumber: fixture.anchorBlockNumber})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cp.blockNumber != fixture.anchorBlockNumber {
		t.Fatalf("checkpoint blockNumber: got %d want %d", cp.blockNumber, fixture.anchorBlockNumber)
	}
	if cp.blockHash != common.HexToHash(testHash("61")) || cp.stateRoot != common.HexToHash(testHash("62")) {
		t.Fatalf("checkpoint hash/stateRoot not returned")
	}
}
```
(The fixture's `anchorInput` already encodes `blockHash=testHash("61")`, `stateRoot=testHash("62")`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run TestValidateManifestBindingCollectsAnchorCheckpoints -count=1`
Expected: FAIL (return type mismatch / compile error).

- [ ] **Step 3: Thread the checkpoint through the return values**

Change the tail of `validateManifestAnchorTransaction` so it returns the checkpoint:
```go
func validateManifestAnchorTransaction(
	view *GuestInputView, tx *types.Transaction, header *types.Header, expected shastaManifestBlock,
) (anchorV4CheckpointView, error) {
	// ...existing checks; on any error `return anchorV4CheckpointView{}, err`...
	checkpoint, err := decodeAnchorV4Checkpoint(tx.Data())
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	if checkpoint.blockNumber != expected.AnchorBlockNumber {
		return anchorV4CheckpointView{}, fmt.Errorf(
			"anchor checkpoint block number mismatch: expected %d got %d",
			expected.AnchorBlockNumber, checkpoint.blockNumber)
	}
	return checkpoint, nil
}
```
Change `validateManifestBlockBinding` to `return validateManifestAnchorTransaction(view, txs[0], header, expected)` as its final statement, and its signature to `(anchorV4CheckpointView, error)`. **Also update the Task 1 test `TestValidateManifestBindingRejectsNonGoldenTouchAnchorSender`**, which calls `validateManifestAnchorTransaction` directly — change `err = validateManifestAnchorTransaction(...)` to `_, err = validateManifestAnchorTransaction(...)`. In `ValidateGuestInputManifestBindingWithContext`, replace the per-block loop body so it collects checkpoints:
```go
	checkpoints := make([]anchorV4CheckpointView, 0, len(derived))
	for index, expectedBlock := range derived {
		if err := ctx.Err(); err != nil {
			return err
		}
		block, witness, err := decodeReplayBlock(view.Witnesses[index].ReplayBlock)
		if err != nil {
			return fmt.Errorf("decode witness block %d: %w", index, err)
		}
		checkpoint, err := validateManifestBlockBinding(ctx, view, proposal, block, witness, expectedBlock, canonicalParent, canonicalGrandparent)
		if err != nil {
			return fmt.Errorf("manifest block %d: %w", index, err)
		}
		checkpoints = append(checkpoints, checkpoint)
		rolledGrandparent := compactAncestorFromHeader(canonicalParent)
		canonicalGrandparent = &rolledGrandparent
		canonicalParent = block.Header()
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/prover/ -run TestValidateManifestBinding -count=1`
Expected: PASS (existing tests still green; checkpoints now collected).

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "refactor(prover): collect shasta anchor checkpoints during binding

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 4: Normal-path L1 linkage validator

**Files:**
- Modify: `internal/prover/manifest_validate.go` (new `validateAnchorL1Linkage`; call it at the end of `ValidateGuestInputManifestBindingWithContext`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Consumes: `checkpoints []anchorV4CheckpointView` (Task 3), `decodeGuestInputL1Headers` (Task 2), existing `sourceSpans []manifestAnchorSourceSpan`, `lastAnchor uint64`, `proposal shastaProposalView`.
- Produces: `func validateAnchorL1Linkage(view *GuestInputView, proposal shastaProposalView, checkpoints []anchorV4CheckpointView, sourceSpans []manifestAnchorSourceSpan, lastAnchor uint64) error`. This task implements only the **normal** path; forced-inclusion prefix (`startIndex > 0`) and bypass are wired in Task 7 — until then, return an explicit error `errAnchorL1ParentCheckpointUnsupported` when they occur so nothing is silently accepted.
- Produces: `func manifestForcedInclusionPrefixCount(sourceSpans []manifestAnchorSourceSpan) int` = sum of `blockCount` over all leading forced-inclusion spans (all spans except the final normal one).

- [ ] **Step 1: Write failing tests (happy path via real fixture + a mismatch negative)**

```go
func TestValidateManifestBindingAcceptsRealFixtureL1Linkage(t *testing.T) {
	view := loadRealFixtureView(t) // helper: DecodeGuestInput on testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("real fixture must pass L1 linkage: %v", err)
	}
}

func TestValidateAnchorL1LinkageRejectsForgedCheckpointStateRoot(t *testing.T) {
	view := loadRealFixtureView(t)
	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		t.Fatalf("proposal: %v", err)
	}
	origin, ancestors, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil || origin == nil || len(ancestors) == 0 {
		t.Fatalf("l1 headers: %v", err)
	}
	// one checkpoint per anchor block number present in ancestors[0], stateRoot forged
	cp := anchorV4CheckpointView{blockNumber: ancestors[0].Number.Uint64(),
		blockHash: ancestors[0].Hash(), stateRoot: common.HexToHash(testHash("ff"))}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	err = validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, ancestors[0].Number.Uint64()-1)
	if err == nil || !strings.Contains(err.Error(), "not found in taiko.l1_ancestor_headers") {
		t.Fatalf("expected forged stateRoot rejection, got %v", err)
	}
}
```
Add `loadRealFixtureView(t)` reading the testdata file (skip with `t.Skip` if absent) and returning `*GuestInputView` via `DecodeGuestInput`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run 'TestValidateAnchorL1Linkage|TestValidateManifestBindingAcceptsRealFixtureL1Linkage' -count=1`
Expected: FAIL ("validateAnchorL1Linkage not defined"; the real-fixture test fails because linkage isn't wired yet — it currently passes without the check, so this test locks in that the check runs).

- [ ] **Step 3: Implement the normal-path validator and wire it in**

```go
var errAnchorL1ParentCheckpointUnsupported = fmt.Errorf(
	"forced-inclusion / stalled-anchor checkpoint binding not yet supported")

func manifestForcedInclusionPrefixCount(sourceSpans []manifestAnchorSourceSpan) int {
	count := 0
	for _, span := range sourceSpans[:max(0, len(sourceSpans)-1)] {
		if span.isForcedInclusion {
			count += span.blockCount
		}
	}
	return count
}

func validateAnchorL1Linkage(
	view *GuestInputView,
	proposal shastaProposalView,
	checkpoints []anchorV4CheckpointView,
	sourceSpans []manifestAnchorSourceSpan,
	lastAnchor uint64,
) error {
	l1Header, ancestors, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil {
		return err
	}
	if l1Header.Number == nil || l1Header.Number.Uint64() != proposal.OriginBlockNumber {
		return fmt.Errorf("taiko.l1_header.number mismatch: expected %d", proposal.OriginBlockNumber)
	}
	if l1Header.Hash() != proposal.OriginBlockHash {
		return fmt.Errorf("taiko.l1_header hash mismatch")
	}

	anchorNumbers := make([]uint64, len(checkpoints))
	for i, cp := range checkpoints {
		anchorNumbers[i] = cp.blockNumber
	}
	if shouldBypassStalledAnchorLinkage(anchorNumbers, lastAnchor, proposal.OriginBlockNumber, view.GuestInputChainID) {
		return errAnchorL1ParentCheckpointUnsupported // wired in Task 7
	}
	startIndex := manifestForcedInclusionPrefixCount(sourceSpans)
	if startIndex > 0 {
		return errAnchorL1ParentCheckpointUnsupported // wired in Task 7
	}
	if startIndex > len(checkpoints) {
		return fmt.Errorf("forced-inclusion prefix exceeds checkpoint count")
	}
	headerCheckpoints := checkpoints[startIndex:]

	if len(ancestors) == 0 {
		return fmt.Errorf("taiko.l1_ancestor_headers must not be empty")
	}
	cpIndex := 0
	var prevNumber *uint64
	var prevHash common.Hash
	var lastNumber uint64
	var lastHash common.Hash
	for i, header := range ancestors {
		if header.Number == nil {
			return fmt.Errorf("taiko.l1_ancestor_headers[%d] missing number", i)
		}
		headerHash := header.Hash()
		number := header.Number.Uint64()
		if prevNumber != nil {
			if number != *prevNumber+1 {
				return fmt.Errorf("taiko.l1_ancestor_headers must be contiguous at index %d", i)
			}
			if header.ParentHash != prevHash {
				return fmt.Errorf("taiko.l1_ancestor_headers parent hash mismatch at index %d", i)
			}
		}
		for cpIndex < len(headerCheckpoints) && headerCheckpoints[cpIndex].blockNumber == number {
			cp := headerCheckpoints[cpIndex]
			if cp.blockHash != headerHash || cp.stateRoot != header.Root {
				return fmt.Errorf(
					"anchor checkpoint (%d) not found in taiko.l1_ancestor_headers", cp.blockNumber)
			}
			cpIndex++
		}
		n := number
		prevNumber = &n
		prevHash = headerHash
		lastNumber = number
		lastHash = headerHash
	}
	if lastNumber != proposal.OriginBlockNumber {
		return fmt.Errorf("taiko.l1_ancestor_headers last block number mismatch: expected %d got %d",
			proposal.OriginBlockNumber, lastNumber)
	}
	if lastHash != proposal.OriginBlockHash {
		return fmt.Errorf("taiko.l1_ancestor_headers last hash mismatch")
	}
	if cpIndex != len(headerCheckpoints) {
		return fmt.Errorf("anchor checkpoint (%d) not found in taiko.l1_ancestor_headers",
			headerCheckpoints[cpIndex].blockNumber)
	}
	return nil
}
```
Add `shouldBypassStalledAnchorLinkage` (port of raiko2 anchor.rs):
```go
func shouldBypassStalledAnchorLinkage(anchorNumbers []uint64, lastAnchor, origin, chainID uint64) bool {
	if len(anchorNumbers) == 0 {
		return false
	}
	first := anchorNumbers[0]
	if first != lastAnchor || origin-first <= anchorMaxOffsetForChain(chainID) {
		return false
	}
	for _, a := range anchorNumbers {
		if a != first {
			return false
		}
	}
	return true
}
```
At the end of `ValidateGuestInputManifestBindingWithContext` (after the block loop), call it:
```go
	if err := validateAnchorL1Linkage(view, proposal, checkpoints, sourceSpans, lastAnchor); err != nil {
		return err
	}
	return nil
```
(`proposal` in scope is `shastaProposalView`; pass it directly. `lastAnchor` is the already-decoded value.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/prover/ -run 'TestValidateAnchorL1Linkage|TestValidateManifestBinding|TestReplay' -count=1`
Expected: PASS. The real fixture (1 normal source, no bypass) exercises the normal path and its 192 identical anchors all match `l1_ancestor_headers[0]`.

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): bind shasta anchor checkpoint to l1 ancestor headers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase 3 — Fix 1 full parity: forced-inclusion / bypass parent checkpoint

### Task 5: CheckpointStore storage-slot derivation

**Files:**
- Modify: `internal/prover/manifest_validate.go`
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Produces: `func shastaCheckpointStorageSlots(blockNumber uint64) (common.Hash, common.Hash)` returning `(blockHashSlot, stateRootSlot)`.
- Const `shastaSignalServiceCheckpointsSlot uint64 = 254`.

- [ ] **Step 1: Write failing test**

```go
func TestShastaCheckpointStorageSlots(t *testing.T) {
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(24862915)
	var buf [64]byte
	new(big.Int).SetUint64(24862915).FillBytes(buf[0:32])
	new(big.Int).SetUint64(254).FillBytes(buf[32:64])
	want := crypto.Keccak256Hash(buf[:])
	if blockHashSlot != want {
		t.Fatalf("blockHashSlot: got %s want %s", blockHashSlot.Hex(), want.Hex())
	}
	wantSR := common.BigToHash(new(big.Int).Add(want.Big(), big.NewInt(1)))
	if stateRootSlot != wantSR {
		t.Fatalf("stateRootSlot: got %s want %s", stateRootSlot.Hex(), wantSR.Hex())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run TestShastaCheckpointStorageSlots -count=1`
Expected: FAIL ("shastaCheckpointStorageSlots not defined").

- [ ] **Step 3: Implement**

```go
const shastaSignalServiceCheckpointsSlot uint64 = 254

func shastaCheckpointStorageSlots(blockNumber uint64) (common.Hash, common.Hash) {
	var buf [64]byte
	new(big.Int).SetUint64(blockNumber).FillBytes(buf[0:32])
	new(big.Int).SetUint64(shastaSignalServiceCheckpointsSlot).FillBytes(buf[32:64])
	blockHashSlot := crypto.Keccak256Hash(buf[:])
	stateRootSlot := common.BigToHash(new(big.Int).Add(blockHashSlot.Big(), big.NewInt(1)))
	return blockHashSlot, stateRootSlot
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/prover/ -run TestShastaCheckpointStorageSlots -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): derive shasta checkpoint store storage slots

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 6: Read the parent checkpoint from L2 CheckpointStore state

**Files:**
- Create: `internal/prover/l2_state.go` (state-read helper, keeps the go-ethereum trie plumbing isolated)
- Modify: `internal/prover/manifest_validate.go` (`verifiedParentShastaCheckpoint`); `internal/prover/guestinput.go` (surface `ProposalStateNodesRaw` on the view if not already reachable via `view.Raw.ProposalStateNodes`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Produces: `func readParentL2Storage(view *GuestInputView, account common.Address, slot common.Hash) (common.Hash, error)` in `l2_state.go`. It builds a `stateless.Witness` from `view.Witnesses[0]`'s state nodes plus `view.Raw.ProposalStateNodes` (both arrays of hex-encoded RLP nodes), materializes it with `MakeHashDB()`, opens `state.New(preStateRoot, ...)` where `preStateRoot` is the first replay block's parent header `Root`, and returns `db.GetState(account, slot)`.
- Produces: `func verifiedParentShastaCheckpoint(view *GuestInputView, blockNumber uint64) (anchorV4CheckpointView, error)` in `manifest_validate.go`.
- Consumes: `shastaCheckpointStorageSlots` (Task 5); `decodeWitness`/`stateless.Witness` patterns from `replay.go` `GethRunner.Execute`.

- [ ] **Step 1: Write failing test**

Build a minimal in-memory trie with the two checkpoint slots set on a CheckpointStore account, encode its nodes as the witness state, and assert the read returns them. Model the trie construction on `replay_test.go`'s witness helpers (use `trie.NewStackTrie`/`triedb` as `replay.go` does). Assert:
```go
func TestReadParentL2StorageReturnsCheckpoint(t *testing.T) {
	view, account, blockNumber, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(blockNumber)
	gotHash, err := readParentL2Storage(view, account, blockHashSlot)
	if err != nil {
		t.Fatalf("read blockHash: %v", err)
	}
	gotRoot, err := readParentL2Storage(view, account, stateRootSlot)
	if err != nil {
		t.Fatalf("read stateRoot: %v", err)
	}
	if gotHash != wantHash || gotRoot != wantRoot {
		t.Fatalf("checkpoint read mismatch")
	}
}
```
`newCheckpointStoreStateFixture` constructs an account trie containing `account` with a storage trie holding `blockHashSlot=>wantHash`, `stateRootSlot=>wantRoot`, sets the first witness's `state` to the full node set and the parent header `Root` to the account-trie root. (Reuse the trie-building approach already present in `replay_test.go`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run 'TestReadParentL2Storage' -count=1`
Expected: FAIL ("readParentL2Storage not defined").

- [ ] **Step 3: Implement `l2_state.go`**

```go
package prover

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/triedb"
)

func readParentL2Storage(view *GuestInputView, account common.Address, slot common.Hash) (common.Hash, error) {
	if len(view.Witnesses) == 0 {
		return common.Hash{}, fmt.Errorf("guest input must include at least one witness")
	}
	_, replayWitness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		return common.Hash{}, fmt.Errorf("decode first witness block: %w", err)
	}
	if len(replayWitness.Witness.Headers) == 0 || replayWitness.Witness.Headers[0] == nil {
		return common.Hash{}, fmt.Errorf("first witness missing parent header")
	}
	preStateRoot := replayWitness.Witness.Headers[0].Root

	witness := &stateless.Witness{
		Headers: replayWitness.Witness.Headers,
		Codes:   replayWitness.Witness.Codes,
		State:   make(map[string]struct{}, len(replayWitness.Witness.State)+len(view.Raw.ProposalStateNodes)),
	}
	for node := range replayWitness.Witness.State {
		witness.State[node] = struct{}{}
	}
	for i, raw := range view.Raw.ProposalStateNodes {
		var hexNode string
		if err := json.Unmarshal(raw, &hexNode); err != nil {
			return common.Hash{}, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		decoded, err := hexutil.Decode(hexNode)
		if err != nil {
			return common.Hash{}, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		witness.State[string(decoded)] = struct{}{}
	}

	memdb := witness.MakeHashDB()
	db, err := state.New(preStateRoot,
		state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)))
	if err != nil {
		return common.Hash{}, fmt.Errorf("open parent state at %s: %w", preStateRoot.Hex(), err)
	}
	return db.GetState(account, slot), nil
}
```
`view.Raw.ProposalStateNodes` is `[]json.RawMessage` of hex strings (confirmed on the wire), so each entry unmarshals to a `string` then `hexutil.Decode`s to the RLP node bytes.

- [ ] **Step 4: Implement `verifiedParentShastaCheckpoint` in `manifest_validate.go`**

```go
func verifiedParentShastaCheckpoint(view *GuestInputView, blockNumber uint64) (anchorV4CheckpointView, error) {
	store, err := decodeWitnessCheckpointStore(view)
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(blockNumber)
	blockHash, err := readParentL2Storage(view, store, blockHashSlot)
	if err != nil {
		return anchorV4CheckpointView{}, fmt.Errorf("read parent CheckpointStore blockHash: %w", err)
	}
	stateRoot, err := readParentL2Storage(view, store, stateRootSlot)
	if err != nil {
		return anchorV4CheckpointView{}, fmt.Errorf("read parent CheckpointStore stateRoot: %w", err)
	}
	if blockHash == (common.Hash{}) {
		return anchorV4CheckpointView{}, fmt.Errorf("parent CheckpointStore blockHash is zero")
	}
	if stateRoot == (common.Hash{}) {
		return anchorV4CheckpointView{}, fmt.Errorf("parent CheckpointStore stateRoot is zero")
	}
	return anchorV4CheckpointView{blockNumber: blockNumber, blockHash: blockHash, stateRoot: stateRoot}, nil
}

func decodeWitnessCheckpointStore(view *GuestInputView) (common.Address, error) {
	if len(view.Witnesses) == 0 {
		return common.Address{}, fmt.Errorf("guest input must include at least one witness")
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	return requireAddress(fields, "checkpoint_store_contract", "checkpointStoreContract")
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/prover/ -run 'TestReadParentL2Storage' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/prover/l2_state.go internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "feat(prover): read parent shasta checkpoint from l2 checkpoint store

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

### Task 7: Wire bypass + forced-inclusion parent-checkpoint matching

**Files:**
- Modify: `internal/prover/manifest_validate.go` (`validateAnchorL1Linkage`)
- Test: `internal/prover/manifest_validate_test.go`

**Interfaces:**
- Consumes: `verifiedParentShastaCheckpoint` (Task 6), `shouldBypassStalledAnchorLinkage`, `manifestForcedInclusionPrefixCount` (Task 4).
- Replaces the two `errAnchorL1ParentCheckpointUnsupported` early returns with real logic. Removes `errAnchorL1ParentCheckpointUnsupported`.

- [ ] **Step 1: Write failing tests (bypass + forced-inclusion)**

```go
func TestValidateAnchorL1LinkageBypassMatchesParentCheckpoint(t *testing.T) {
	view, account, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	_ = account
	proposal := shastaProposalView{
		OriginBlockNumber: parentAnchor + 600, // > mainnet offset 512 beyond parentAnchor
		OriginBlockHash:   fixtureOriginHash(t, view),
	}
	cp := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	// origin header must be present as l1_header for the origin checks; helper sets chainID 167000.
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, parentAnchor); err != nil {
		t.Fatalf("bypass path should accept matching parent checkpoint: %v", err)
	}
	bad := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: common.HexToHash(testHash("ee"))}
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{bad}, spans, parentAnchor); err == nil {
		t.Fatalf("bypass path must reject non-matching checkpoint")
	}
}
```
(`newCheckpointStoreStateFixture` and `fixtureOriginHash` must also populate `taiko.l1_header` = an origin header at `OriginBlockNumber` whose hash is `OriginBlockHash`; extend the helper accordingly. A forced-inclusion test mirrors this with `spans = [{forced,1},{normal,1}]` and `startIndex==1`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/prover/ -run TestValidateAnchorL1LinkageBypass -count=1`
Expected: FAIL (currently returns `errAnchorL1ParentCheckpointUnsupported`).

- [ ] **Step 3: Replace the early returns with parent-checkpoint logic**

In `validateAnchorL1Linkage`, after the `l1Header` origin checks, replace the bypass early-return:
```go
	if shouldBypassStalledAnchorLinkage(anchorNumbers, lastAnchor, proposal.OriginBlockNumber, view.GuestInputChainID) {
		parentCheckpoint, err := verifiedParentShastaCheckpoint(view, lastAnchor)
		if err != nil {
			return err
		}
		for _, cp := range checkpoints {
			if cp != parentCheckpoint {
				return fmt.Errorf("anchor checkpoint (%d) does not match parent checkpoint (%d)",
					cp.blockNumber, parentCheckpoint.blockNumber)
			}
		}
		return nil
	}
```
And replace the forced-inclusion prefix early-return:
```go
	startIndex := manifestForcedInclusionPrefixCount(sourceSpans)
	if startIndex > len(checkpoints) {
		return fmt.Errorf("forced-inclusion prefix exceeds checkpoint count")
	}
	if startIndex > 0 {
		parentCheckpoint, err := verifiedParentShastaCheckpoint(view, lastAnchor)
		if err != nil {
			return err
		}
		for _, cp := range checkpoints[:startIndex] {
			if cp != parentCheckpoint {
				return fmt.Errorf("forced-inclusion anchor checkpoint (%d) does not match parent checkpoint (%d)",
					cp.blockNumber, parentCheckpoint.blockNumber)
			}
		}
	}
	headerCheckpoints := checkpoints[startIndex:]
```
Delete the `errAnchorL1ParentCheckpointUnsupported` declaration.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/prover/ -count=1`
Expected: PASS (full package, including real-fixture regression and the new bypass/forced-inclusion tests).

- [ ] **Step 5: Commit**

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix(prover): bind forced-inclusion and stalled anchor checkpoints via l2 state

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Full suite**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Confirm the real fixture still replays**

Run: `go test ./internal/prover/ -run TestReplay -count=1`
Expected: PASS (Fix 1 adds L1 linkage on top of the existing replay; the real fixture satisfies it).

## Notes for the executor

- The Fix 2 non-test code is captured at `scratchpad/fix2-anchor-envelope-wip.patch` (`git apply --check` passes); use it as a cross-check, but the test code must be written per Task 1.
- The real fixture (`testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`, ~38MB) has 1 normal source, no forced inclusion, and `origin - anchor = 37 ≤ 512`, so it exercises **only** the normal path. Tasks 6-7 (bypass / forced-inclusion) are covered by synthetic fixtures, not the real one.
- Keep `verifiedParentShastaCheckpoint` reading slot base `254` (single mapping) to match raiko2 / the deployed contract; taiko-mono's `SignalService` now nests `[VERSION][blockNumber]`, which is a newer version and must not be copied.
- Fix 3 (compact-ancestor BLOCKHASH window) is out of scope — do not touch the `replayChainContext` / `CompactAncestor` paths.
