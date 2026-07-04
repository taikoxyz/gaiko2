# Gaiko2 Disable Compact Replay Ancestors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `gaiko2` refuse hash-only ("compact") replay ancestors and authenticate every replay-ancestor hash by keccak recomputation, closing a `BLOCKHASH` spoofing soundness hole.

**Architecture:** The replay witness carries a parent header plus ancestor headers that feed `taiko-geth`'s `BLOCKHASH` header walk. Today deeper ancestors arrive as `{number, hash, parentHash, timestamp}` with the hash trusted verbatim. This plan requires every ancestor to be a full header (self-authenticating: `taiko-geth` keys it by recomputed `header.Hash()`), reorders them parent-first, and validates ancestor contiguity by recomputed hash — mirroring the `raiko2` guest and the fixed `raiko2` producer (PR #136).

**Tech Stack:** Go, `taiko-geth` stateless replay (`core/stateless`, `core.GetHashFn`), Go `testing`.

**Design doc:** [docs/plans/2026-07-04-gaiko2-disable-compact-replay-ancestors-design.md](2026-07-04-gaiko2-disable-compact-replay-ancestors-design.md)

## Global Constraints

- Language/runtime: Go; `taiko-geth` is pinned via a `replace` directive in `go.mod` — do not change it.
- Change **only** the replay-witness ancestor path (`ReplayWitness`, `decodeWitnessHeaders`, `validateReplayWitness`, `newReplayChainContext`). Do **not** touch the manifest proposal-ancestor path (`decodeProposalAncestorHeaderContext`, `validateManifestHeaderAncestry`) — it already requires full headers.
- Keep the `CompactAncestor` **type** in `internal/prover/types.go`; it is still used by the manifest base-fee path via `compactAncestorFromHeader`. Only the `ReplayWitness.CompactAncestors` **field** and the untrusted wire decoding of compact ancestors are removed.
- Fail closed: reject malformed or compact input with an error; never best-effort.
- Do not add an EIP-2935 history-contract `BLOCKHASH` path (`taiko-geth`'s `opBlockhash` uses the header walk).
- Work happens on branch `fix/disable-compact-replay-ancestors` (already created off `main`).
- End every commit message with the trailer:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 1: Reject compact replay ancestors; require full, parent-first ancestor headers

**Files:**
- Modify: `internal/prover/decode.go` (`decodeWitnessHeaders`, `decodeWitness`)
- Test: `internal/prover/decode_ancestors_test.go` (create)
- Modify (skip only): `internal/prover/replay_fixture_test.go`, `internal/api/server_test.go`

**Interfaces:**
- Produces: `decodeWitnessHeaders(raws []json.RawMessage) ([]*types.Header, error)` — returns all ancestor headers as full `*types.Header`, ordered parent-first (`Headers[0]` = parent, i.e. highest block number), or an error if any entry is compact.
- Consumes: `decodeHeader`, `rawWitnessHeader`, `stateless.Witness` (existing).

After this task the deeper ancestors are self-authenticating: `newReplayChainContext` (unchanged here) adds every `Witness.Headers` entry under its recomputed `header.Hash()`, so `BLOCKHASH` cannot be fed an attacker-chosen hash. Explicit linkage validation and dead-code removal follow in Task 2.

- [ ] **Step 1: Write the failing tests**

Create `internal/prover/decode_ancestors_test.go`:

```go
package prover

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// A compact (hash-only) replay ancestor must be rejected: its hash is
// attacker-controlled and would let BLOCKHASH be spoofed.
func TestDecodeWitnessRejectsCompactAncestor(t *testing.T) {
	witnessJSON := mustRawMessage(t, `{
		"state": [], "state_indices": [], "codes": [],
		"headers": [
			{"number": "0x28", "hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "parent_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "timestamp": "0x0"},
			{"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "header": {
				"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0", "number": "0x29", "gasLimit": "0x0", "gasUsed": "0x0",
				"timestamp": "0x0", "extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000", "baseFeePerGas": "0x1"
			}}
		]
	}`)

	if _, err := decodeWitness(witnessJSON); err == nil {
		t.Fatalf("expected compact replay ancestor to be rejected")
	}
}

// Full ancestors arrive oldest-first (parent last); the decoder must return them
// parent-first so taiko-geth's Root()/Headers[0]==parent invariant holds.
func TestDecodeWitnessOrdersFullAncestorsParentFirst(t *testing.T) {
	witnessJSON := mustRawMessage(t, `{
		"state": [], "state_indices": [], "codes": [],
		"headers": [`+fullWitnessHeaderJSON(41, "0x41")+`,`+fullWitnessHeaderJSON(42, "0x42")+`]
	}`)

	witness, err := decodeWitness(witnessJSON)
	if err != nil {
		t.Fatalf("decode witness: %v", err)
	}
	if len(witness.Witness.Headers) != 2 {
		t.Fatalf("unexpected header count: %d", len(witness.Witness.Headers))
	}
	if witness.Witness.Headers[0].Number.Uint64() != 42 {
		t.Fatalf("parent not first: got number %d", witness.Witness.Headers[0].Number.Uint64())
	}
	if witness.Witness.Root() != witness.Witness.Headers[0].Root {
		t.Fatalf("Root() must equal parent (Headers[0]) root")
	}
}

// With full ancestors, taiko-geth's BLOCKHASH walk returns the real ancestor hashes.
func TestReplayChainContextServesRealAncestorBlockhash(t *testing.T) {
	grandparent := &types.Header{
		Number:     big.NewInt(40),
		Time:       100,
		Root:       common.HexToHash(testHash("a0")),
		ParentHash: common.HexToHash(testHash("9f")),
	}
	parent := &types.Header{
		Number:     big.NewInt(41),
		Time:       101,
		Root:       common.HexToHash(testHash("a1")),
		ParentHash: grandparent.Hash(),
	}
	child := &types.Header{
		Number:     big.NewInt(42),
		Time:       102,
		Root:       common.HexToHash(testHash("a2")),
		ParentHash: parent.Hash(),
	}

	witness := &ReplayWitness{Witness: &stateless.Witness{Headers: []*types.Header{parent, grandparent}}}
	chain := newReplayChainContext(&params.ChainConfig{}, types.NewBlockWithHeader(child), witness)
	getHash := core.GetHashFn(child, chain)

	if got := getHash(41); got != parent.Hash() {
		t.Fatalf("BLOCKHASH(41): got %s want %s", got.Hex(), parent.Hash().Hex())
	}
	if got := getHash(40); got != grandparent.Hash() {
		t.Fatalf("BLOCKHASH(40): got %s want %s", got.Hex(), grandparent.Hash().Hex())
	}
	if got := getHash(39); got != grandparent.ParentHash {
		t.Fatalf("BLOCKHASH(39): got %s want %s", got.Hex(), grandparent.ParentHash.Hex())
	}
}

// fullWitnessHeaderJSON builds a minimal full witness header (with a "header" field)
// at the given number; distinct roots keep hashes unique across numbers.
func fullWitnessHeaderJSON(number uint64, rootPrefix string) string {
	root := rootPrefix + "00000000000000000000000000000000000000000000000000000000000000"
	root = root[:66]
	return `{"header": {
		"parentHash": "0x` + "00" + `00000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "` + root + `",
		"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0", "number": "` + hexUint(number) + `", "gasLimit": "0x0", "gasUsed": "0x0",
		"timestamp": "0x0", "extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000", "baseFeePerGas": "0x1"
	}}`
}

func hexUint(v uint64) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0x0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return "0x" + string(buf[i:])
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/prover/ -run 'TestDecodeWitnessRejectsCompactAncestor|TestDecodeWitnessOrdersFullAncestorsParentFirst|TestReplayChainContextServesRealAncestorBlockhash' -v`

Expected: FAIL — `TestDecodeWitnessRejectsCompactAncestor` fails because compact ancestors are currently accepted; the ordering test fails because `Headers[0]` is currently the oldest header.

- [ ] **Step 3: Rewrite `decodeWitnessHeaders` to reject compact and order parent-first**

In `internal/prover/decode.go`, replace the whole `decodeWitnessHeaders` function with:

```go
func decodeWitnessHeaders(raws []json.RawMessage) ([]*types.Header, error) {
	if len(raws) == 0 {
		return nil, fmt.Errorf("witness must include at least one ancestor header")
	}
	headers := make([]*types.Header, 0, len(raws))
	for index, raw := range raws {
		var decoded rawWitnessHeader
		if err := json.Unmarshal(raw, &decoded); err != nil ||
			len(decoded.Header) == 0 || bytes.Equal(decoded.Header, []byte("null")) {
			return nil, fmt.Errorf(
				"witness header %d is not a full header; compact replay ancestors are not accepted",
				index,
			)
		}
		header, err := decodeHeader(decoded.Header)
		if err != nil {
			return nil, fmt.Errorf("decode witness header %d: %w", index, err)
		}
		headers = append(headers, header)
	}
	// Producers emit ancestors oldest-first (parent last); taiko-geth's stateless
	// witness requires parent-first (Headers[0] = parent, used by Root()). Reverse
	// into parent-first order. validateReplayWitness then binds Headers[0] to the
	// block's parent and verifies ancestor contiguity, failing closed on any
	// ordering deviation.
	for i, j := 0, len(headers)-1; i < j; i, j = i+1, j-1 {
		headers[i], headers[j] = headers[j], headers[i]
	}
	return headers, nil
}
```

- [ ] **Step 4: Update `decodeWitness` for the new signature**

In `internal/prover/decode.go`, inside `decodeWitness`, replace the header-decoding block and the return so it no longer references compact ancestors:

```go
	fullHeaders, err := decodeWitnessHeaders(decoded.Headers)
	if err != nil {
		return nil, err
	}
	witness := &stateless.Witness{
		Headers: fullHeaders,
		Codes:   make(map[string]struct{}, len(decoded.Codes)),
		State:   make(map[string]struct{}, len(decoded.State)),
	}
	for _, code := range decoded.Codes {
		witness.Codes[string(code)] = struct{}{}
	}
	for _, node := range decoded.State {
		witness.State[string(node)] = struct{}{}
	}
	return &ReplayWitness{Witness: witness}, nil
```

(The `ReplayWitness.CompactAncestors` field is left zero-valued here and removed in Task 2. `validateReplayWitness` and `newReplayChainContext` still compile: their compact loops iterate a nil slice and no-op.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/prover/ -run 'TestDecodeWitnessRejectsCompactAncestor|TestDecodeWitnessOrdersFullAncestorsParentFirst|TestReplayChainContextServesRealAncestorBlockhash' -v`

Expected: PASS (all three).

- [ ] **Step 6: Skip the shared-fixture tests pending fixture regeneration**

The mainnet fixture still carries compact ancestors, so tests that decode it through the real path now fail by design. Add a skip as the first line of each test body below, with a TODO referencing the follow-up.

In `internal/prover/replay_fixture_test.go`, add to the top of `TestSharedShastaFixtureMetadata`, `TestSharedShastaFixtureReplaysStateless`, and `TestSharedShastaFixtureFirstAnchorTransactionDecodes`:

```go
	// TODO(compact-ancestors): re-enable after regenerating testdata/shasta_request_*.json
	// with full replay ancestor headers (raiko2 dump_gaiko2_shasta_fixture). See
	// docs/plans/2026-07-04-gaiko2-disable-compact-replay-ancestors-design.md.
	t.Skip("shared fixture carries compact replay ancestors; pending regeneration to full headers")
```

In `internal/api/server_test.go`, add the same block to the top of `TestNewServerReturnsSuccessEnvelope` and `TestNewServerLogsProveSuccess` (the two that POST the unmodified shared fixture and expect HTTP 200).

- [ ] **Step 7: Run the full suite and confirm only the intended skips remain**

Run: `go test ./...`

Expected: PASS. The five tests above report `--- SKIP`. If any other test fails **solely** because the shared mainnet fixture carries compact ancestors, add the same skip block and TODO to it; do not skip a test that fails for any other reason.

- [ ] **Step 8: Commit**

```bash
git add internal/prover/decode.go internal/prover/decode_ancestors_test.go \
  internal/prover/replay_fixture_test.go internal/api/server_test.go
git commit -m "$(cat <<'EOF'
fix(prover): reject compact replay ancestors, require full headers

Compact (hash-only) replay ancestors let a malicious packet spoof
BLOCKHASH(N-3..N-256): the hash was trusted verbatim and fed into
taiko-geth's header-walk blockhash lookup. Require every replay ancestor
to be a full header (self-authenticating via recomputed header.Hash())
and order them parent-first. Mirrors raiko2 PR #136 on the consumer side.

Shared-fixture tests are skipped pending regeneration to full ancestor
headers.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Authenticate ancestor linkage by recomputed hash; remove the dead compact path

**Files:**
- Modify: `internal/prover/replay.go` (`validateReplayWitness`, `newReplayChainContext`)
- Modify: `internal/prover/types.go` (`ReplayWitness`)
- Test: `internal/prover/replay_ancestors_test.go` (create)

**Interfaces:**
- Consumes: `decodeWitnessHeaders` producing parent-first full headers (Task 1).
- Produces: `ReplayWitness` with a single field `Witness *stateless.Witness`; `validateReplayWitness` enforces full-header ancestor contiguity by recomputed hash.

- [ ] **Step 1: Write the failing tests**

Create `internal/prover/replay_ancestors_test.go`:

```go
package prover

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
)

func ancestorChainWitness(parent, grandparent *types.Header) *ReplayWitness {
	return &ReplayWitness{Witness: &stateless.Witness{Headers: []*types.Header{parent, grandparent}}}
}

// A well-formed full-ancestor chain (parent-first, hash-linked) is accepted.
func TestValidateReplayWitnessAcceptsFullAncestorChain(t *testing.T) {
	grandparent := &types.Header{Number: big.NewInt(40), Root: common.HexToHash(testHash("a0")), ParentHash: common.HexToHash(testHash("9f"))}
	parent := &types.Header{Number: big.NewInt(41), Root: common.HexToHash(testHash("a1")), ParentHash: grandparent.Hash()}
	child := &types.Header{Number: big.NewInt(42), Root: common.HexToHash(testHash("a2")), ParentHash: parent.Hash()}
	block := types.NewBlockWithHeader(child)

	if err := validateReplayWitness(block, ancestorChainWitness(parent, grandparent)); err != nil {
		t.Fatalf("expected valid full ancestor chain, got %v", err)
	}
}

// A grandparent whose recomputed hash does not match parent.ParentHash is rejected;
// this is the old spoof, now caught because the hash is recomputed, not trusted.
func TestValidateReplayWitnessRejectsBrokenAncestorLinkage(t *testing.T) {
	grandparent := &types.Header{Number: big.NewInt(40), Root: common.HexToHash(testHash("a0")), ParentHash: common.HexToHash(testHash("9f"))}
	parent := &types.Header{Number: big.NewInt(41), Root: common.HexToHash(testHash("a1")), ParentHash: common.HexToHash(testHash("de"))} // wrong: != grandparent.Hash()
	child := &types.Header{Number: big.NewInt(42), Root: common.HexToHash(testHash("a2")), ParentHash: parent.Hash()}
	block := types.NewBlockWithHeader(child)

	if err := validateReplayWitness(block, ancestorChainWitness(parent, grandparent)); err == nil {
		t.Fatalf("expected broken ancestor linkage to be rejected")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/prover/ -run 'TestValidateReplayWitnessAcceptsFullAncestorChain|TestValidateReplayWitnessRejectsBrokenAncestorLinkage' -v`

Expected: FAIL to compile or FAIL — `validateReplayWitness` still references `witness.CompactAncestors` (removed next) and does not yet validate full-header linkage, so the broken-linkage case is not rejected.

- [ ] **Step 3: Replace the compact-ancestor validation with full-header linkage**

In `internal/prover/replay.go`, in `validateReplayWitness`, delete the two compact blocks (the `for index := 1; index < len(witness.CompactAncestors)` loop and the `if len(witness.CompactAncestors) > 0 { ... }` tail block) and replace them with a full-header contiguity check over `witness.Witness.Headers`:

```go
	for index := 1; index < len(witness.Witness.Headers); index++ {
		newer := witness.Witness.Headers[index-1]
		older := witness.Witness.Headers[index]
		if older.Number == nil {
			return fmt.Errorf("ancestor header %d is missing number", index)
		}
		if newer.Number.Uint64() != older.Number.Uint64()+1 {
			return fmt.Errorf(
				"ancestor header %d number mismatch: got %d expected %d",
				index,
				older.Number.Uint64(),
				newer.Number.Uint64()-1,
			)
		}
		if newer.ParentHash != older.Hash() {
			return fmt.Errorf(
				"ancestor header %d hash mismatch: got %s expected %s",
				index,
				older.Hash().Hex(),
				newer.ParentHash.Hex(),
			)
		}
	}
	return nil
```

Keep the existing parent binding above it unchanged (`parent := witness.Witness.Headers[0]`; number, `parent.Hash() == block.ParentHash`, and `witness.Witness.Root() == parent.Root` checks).

- [ ] **Step 4: Remove the compact loop from `newReplayChainContext`**

In `internal/prover/replay.go`, in `newReplayChainContext`, delete the `for _, ancestor := range witness.CompactAncestors { ... }` loop and drop the `+ len(witness.CompactAncestors)` terms from the two `make(map[...], ...)` size hints, so it reads:

```go
	ctx := &replayChainContext{
		config:         config,
		engine:         beacon.New(ethash.NewFaker()),
		current:        block.Header(),
		headersByHash:  make(map[common.Hash]*types.Header, len(witness.Witness.Headers)),
		hashesByNumber: make(map[uint64]common.Hash, len(witness.Witness.Headers)),
	}
	for _, header := range witness.Witness.Headers {
		ctx.addHeader(header.Hash(), header)
	}
	return ctx
```

- [ ] **Step 5: Remove the `CompactAncestors` field from `ReplayWitness`**

In `internal/prover/types.go`, change `ReplayWitness` to:

```go
type ReplayWitness struct {
	Witness *stateless.Witness
}
```

Leave the `CompactAncestor` type below it unchanged (still used by the manifest base-fee path).

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./internal/prover/ -run 'TestValidateReplayWitnessAcceptsFullAncestorChain|TestValidateReplayWitnessRejectsBrokenAncestorLinkage' -v`

Expected: PASS (both).

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`

Expected: PASS, with only the five fixture tests from Task 1 skipped. `go vet ./...` should also be clean (no unused imports/identifiers left by the removals).

- [ ] **Step 8: Commit**

```bash
git add internal/prover/replay.go internal/prover/types.go internal/prover/replay_ancestors_test.go
git commit -m "$(cat <<'EOF'
fix(prover): validate full replay ancestor linkage by recomputed hash

Replace the compact-ancestor adjacency check with a full-header contiguity
check (newer.ParentHash == older.Hash(), recomputed) and remove the now
dead ReplayWitness.CompactAncestors field and its chain-context loop. A
spoofed ancestor now fails validation instead of being trusted.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```

---

## Follow-up (out of scope for this plan)

Regenerate `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json` from `raiko2` `origin/main`'s `dump_gaiko2_shasta_fixture` example (full ancestor headers, minified JSON) and remove the `t.Skip` lines added in Task 1. Tracked separately; it depends on building `raiko2` and produces a larger fixture.

## Self-Review

**Spec coverage:**
- Data model — `CompactAncestors` field removed (Task 2, Step 5); type retained. ✓
- Decoder fail-closed + parent-first — Task 1, Steps 3–4. ✓
- Validation by recomputed hash — Task 2, Step 3. ✓
- Chain context compact loop removed — Task 2, Step 4. ✓
- Synthetic tests (reject compact, hash-mismatch, well-formed, blockhash) — Task 1 Step 1, Task 2 Step 1. ✓
- Existing single-full-header tests keep passing — verified via `go test ./...` (Task 1 Step 7, Task 2 Step 7). ✓
- Fixture tests skipped with TODO — Task 1, Step 6. ✓
- Manifest path untouched — Global Constraints; no manifest files in any task's file list. ✓

**Placeholder scan:** No TBD/TODO-as-work; the only `TODO(compact-ancestors)` is an intentional skip marker with a follow-up reference. Every code step shows complete code. ✓

**Type consistency:** `decodeWitnessHeaders` returns `([]*types.Header, error)` in Task 1 and is consumed with that signature in `decodeWitness` (Task 1) and relied on by Task 2. `ReplayWitness{Witness: ...}` construction (Task 1 Step 4) matches the field set after removal (Task 2 Step 5). `newReplayChainContext` and `validateReplayWitness` signatures are unchanged. ✓
