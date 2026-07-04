# gaiko2 Shasta soundness fixes — design

Date: 2026-07-04
Status: design (approved; Fix 3 deferred)
Reference implementation: `raiko2` (`/Users/davidcai/taiko/raiko2`)

**Scope of this PR: Fix 1 + Fix 2.** Fix 3 (BLOCKHASH full-header window) is fully
designed below but deferred to a separate PR by owner decision — it needs a
raiko2-side adapter change and replay-fixture regeneration.

## Context

`gaiko2` replays Shasta execution packets with `taiko-geth` and signs a TEE proof
envelope. Its soundness job is to sign **only** transitions that a correct
derivation from an L1-committed proposal would produce. A TEE signature on a
non-canonical transition is accepted by the L1 Shasta inbox as final, so any
place where `gaiko2` trusts request-supplied data instead of authenticating it is
a soundness hole.

Three such holes were found and confirmed against the code, the go-ethereum
`BLOCKHASH` implementation, the Shasta protocol contracts, and the
`alethia-reth`/`taiko-geth` anchor rules. `raiko2` is the reference prover that
`gaiko2` must be soundness-equivalent to; every fix below makes `gaiko2` match a
check `raiko2` already performs.

Shared root cause: **`gaiko2` authenticates the *shape* of request data
(adjacency, field presence, one committed number) but not its *binding to reality*
(L1 state, real block hashes).**

## Findings (confirmed)

| # | Finding | Severity | Status |
|---|---------|----------|--------|
| 1 | anchorV4 checkpoint `blockHash`/`stateRoot` never bound to L1 | Critical | fix in this PR |
| 2 | anchor tx envelope validation incomplete | High | in working tree; harden + commit this PR |
| 3 | compact replay ancestors let a request forge `BLOCKHASH` inputs | High | designed; deferred to follow-up PR |

### Finding 1 — anchor L1 checkpoint unbound

`validateManifestAnchorTransaction`
([internal/prover/manifest_validate.go:991](internal/prover/manifest_validate.go)) decodes
the anchorV4 checkpoint `(blockNumber, blockHash, stateRoot)` but compares only
`blockNumber`. `blockHash` and `stateRoot` are decoded at
[manifest_validate.go:1034](internal/prover/manifest_validate.go) and discarded.

Nothing else catches it: the L1 inbox commits only `originBlockHash`; the Shasta
`Derivation.md` explicitly delegates anchor-hash/state-root correctness to "the
node/driver"; and the node consensus (`alethia-reth`, `taiko-geth`) validates only
the anchor tx envelope, not the checkpoint contents. Because the GoldenTouch key
(`0x0000777735367b36bC9B61C50022d9D0700dB4Ec`) is public, anyone can sign a
well-formed anchor tx carrying a forged L1 checkpoint. The stored L1 `stateRoot`
backs `SignalService._verifySignalReceived`, so a forged state root enables
fraudulent L1→L2 signal/bridge proofs.

### Finding 2 — anchor tx envelope

The uncommitted working-tree change already adds the missing envelope checks
(type == EIP-1559, GoldenTouch sender recovery, canonical recipient via
`shastaTaikoL2Address`, gas == 1,000,000, `maxFeePerGas == baseFee`, tip == 0,
value == 0, empty access list) and passes tests. Cross-checked against
`raiko2::validate_anchor_transaction_common` (guest-common/src/lib.rs:565-655),
these match every field the node enforces. Residual work only.

### Finding 3 — forgeable BLOCKHASH via compact ancestors

`decodeWitnessHeaders`
([internal/prover/decode.go:439](internal/prover/decode.go)) accepts "compact"
ancestor entries — `{number, parent_hash, timestamp, hash}` with **no full
header** — and takes the declared `hash` on trust. `validateReplayWitness`
([internal/prover/replay.go:314](internal/prover/replay.go)) only checks adjacency
(`current.ParentHash == prev.Hash`) plus a single tail anchor to the real parent.
These hashes feed `replayChainContext` (replay.go:486-546), which is what
go-ethereum's `GetHashFn` walks to serve the `BLOCKHASH` opcode.

Result: `BLOCKHASH(N-1)` and `BLOCKHASH(N-2)` are pinned to authenticated data,
but `BLOCKHASH(N-3 … N-256)` return request-chosen values. The live fixture ships
256 compact ancestors per block, so this is the real data path.

The Shasta anchor contract's `ancestorsHash` (Anchor.sol:262-297) hashes
`blockhash(N-2 … N-256)` against authenticated L2 state and would catch the forgery
— but only if the anchor tx is required to **succeed**. taiko-geth stores a
reverted anchor's error in the receipt and returns no execution error
(state_transition.go:617-620), and `gaiko2` never checks anchor success, so a
forged blockhash makes the anchor silently revert while a *later* tx in the same
block still reads the forged value.

## Reference model (raiko2)

- **L1 anchor binding:** `raiko2` carries the L1 header chain in the guest input
  (`taiko.l1_header` + `taiko.l1_ancestor_headers`), verifies chain contiguity,
  verifies the tip hash equals `proposal.originBlockHash`, and matches each anchor
  checkpoint's `(blockHash, stateRoot)` to the L1 header at its `blockNumber`
  (`validate_l1_anchor_linkage`, guest-common/src/lib.rs:181-342;
  `hydrate_shasta_l1_headers`, pipeline/…/shasta/spec.rs:1427-1590).
- **BLOCKHASH authentication:** `raiko2` requires **full** ancestor headers,
  **recomputes** each hash from the header and ignores the host-supplied hash
  (`WitnessHeader` deserialize discards `hash`, re-derives via `header.hash_slow()`;
  primitives/src/stateless.rs:132-137, test at 565-578), chain-verifies them
  (`compute_ancestor_hashes_for_child`, stateless/src/validation.rs:487-511), and
  **rejects compact ancestors** (`ensure_full_ancestor_headers` →
  `CompactAncestorHeaderUnsupported`, validation.rs:149-160; test
  `rejects_compact_ancestor_headers`, validation.rs:730). For a multi-block
  proposal it uses one shared full-header window rolled forward per block
  (`validate_block_with_ancestor_headers` + `roll_proposal_ancestor_headers_in_place`).

## Design

### Fix 1 — bind the anchor L1 checkpoint to L1

**Data is already on the wire.** The `taiko` blob already contains `l1_header`
(the origin L1 header) and `l1_ancestor_headers` (full L1 headers down to the
lowest anchor block; 38 entries in the fixture). Today `gaiko2` never parses them.

Add an L1 anchor-linkage validator, invoked from
`ValidateGuestInputManifestBinding` after the derived blocks and their anchor
checkpoints are known. It mirrors `raiko2::validate_l1_anchor_linkage`:

1. Decode `taiko.l1_header` and `taiko.l1_ancestor_headers` into full L1 headers.
2. Verify the L1 headers form one contiguous chain (parent-hash linkage + number
   continuity), joined to `l1_header` at the top.
3. Anchor to real L1: require `l1_header.Hash() == proposal.OriginBlockHash`.
   `OriginBlockHash` is inside the proposal preimage → `ProposalHash` → the signed
   transition the L1 inbox verifies, so it is already bound to real L1.
4. Build `number → (blockHash, stateRoot)` over the authenticated chain. For each
   derived block, require the anchorV4 checkpoint's `blockHash` **and** `stateRoot`
   (currently discarded) to equal the L1 header at its `anchorBlockNumber`.
5. Port the stalled-anchor bypass (`raiko2` anchor.rs:41-56): when every anchor
   equals the parent anchor and `origin - lastAnchor > MAX_ANCHOR_OFFSET`, only the
   origin header is required and per-anchor matching is skipped.

Existing anchor-number progression checks (`validateManifestAnchorProgression`,
range `[origin - offset, origin]`, forced-inclusion reuse, offsets 128/512) already
match `raiko2` and stay as-is.

### Fix 2 — complete + land the anchor tx envelope

Keep the working-tree envelope checks (they already match `raiko2`). Anchor nonce
is **not** re-checked in the binding layer — replay's EVM nonce enforcement is
authoritative and functionally equivalent (decision: rely on replay). Add one
hardening check to match `raiko2`: verify `shastaTaikoL2Address(chainID)` resolves
to the expected TaikoL2/anchor contract for supported chains. Then commit with its
tests.

### Fix 3 — authenticate BLOCKHASH via a full-header window (raiko2 way) — DEFERRED

> **Deferred to a follow-up PR** (owner decision, 2026-07-04). Design retained
> here in full. It requires a raiko2-side adapter change and fixture regeneration,
> so it is not bundled with Fix 1/Fix 2. It remains a confirmed High-severity hole
> until shipped.

Adopt `raiko2`'s model exactly: **BLOCKHASH is served only from full ancestor
headers whose hashes are recomputed and chain-verified; compact ancestors are
rejected for this path.**

Replay path changes:
1. **Reject compact ancestors** in the BLOCKHASH-serving path. Any ancestor used
   to serve `BLOCKHASH` must be a full header; a compact entry is a hard error
   (mirror `CompactAncestorHeaderUnsupported`).
2. **Recompute, never trust, the hash.** Key `replayChainContext` entries by
   `header.Hash()` recomputed from the full header (gaiko2 already does this for
   full headers; the fix is to remove the compact path that trusts a declared
   hash).
3. **Full 256-header window, chain-verified.** Maintain a shared proposal-level
   ancestor window of full headers (from `proposal_ancestor_headers`), chain-verify
   it (parent-hash + number continuity) anchored to the first proven block's parent
   (bound via the transition `ParentBlockHash`), and **roll it forward** as each
   block is proven (append the just-verified block header, drop the oldest), like
   `roll_proposal_ancestor_headers_in_place`. Serve `BLOCKHASH` from this window.
4. Remove `CompactAncestor` from the BLOCKHASH data path (`replayChainContext`,
   `validateReplayWitness` tail/adjacency logic). Compact headers may remain **only**
   for metadata-only uses that never feed `BLOCKHASH` or the pre-state root (e.g.
   the proposal parent/grandparent base-fee context in
   `decodeProposalAncestorHeaderContext`), matching how `raiko2` still allows
   compact forms outside the execution path.

**Cross-repo dependency:** the `raiko2 → gaiko2` request adapter must emit **full**
headers in the shared ancestor window (`proposal_ancestor_headers` is currently 255
compact + 1 full). A shared full-header window is smaller on the wire than today's
per-block compact set (~256 full headers for the whole proposal vs. 256 compact × N
blocks), so this is a net payload reduction.

## Cross-cutting

- **Fix 1 reuses the existing fixture.** The checked-in replay fixture is a real
  mainnet proposal, so its `l1_ancestor_headers` and anchor checkpoints are
  internally consistent and should pass Fix 1's new L1-linkage validator as the
  happy path; Fix 1 adds negative tests around it.
- **Fixture regeneration (Fix 3 only, deferred).** Fix 3's full-header window
  rejects the fixture's compact ancestors, so that fixture must be regenerated from
  `raiko2` with full ancestor headers once the adapter emits them. Not needed for
  this PR.
- **Error taxonomy.** Introduce explicit errors mirroring `raiko2` — anchor L1
  mismatch for this PR; `CompactAncestorHeaderUnsupported` / `InvalidAncestorChain`
  when Fix 3 lands — so failures are diagnosable.

## Testing (this PR)

- Fix 1: reject an anchor whose checkpoint `blockHash` or `stateRoot` mismatches
  the L1 header; reject a broken/non-contiguous L1 header chain; reject
  `l1_header.Hash() != originBlockHash`; accept the real fixture as the happy path;
  cover the stalled-anchor bypass.
- Fix 2: keep the three new rejection tests; add the L2-address sanity-check case.

Fix 3 tests (deferred): reject a compact ancestor in the BLOCKHASH path; reject a
full-header window with a broken parent-hash/number chain; prove `BLOCKHASH(N-3)`
resolves to the authenticated hash and a forged one is rejected; regenerate and
pass the full replay regression.

## Out of scope

- Changing the L1 inbox or anchor contracts (the binding is the prover's job).
- The aggregate proof path (`aggregate_validate.go`) — it verifies already-signed
  sub-proofs and does not re-derive blocks; the individual proving path is the
  gate.
- Non-soundness performance work.

## Decisions

- Fix 2 nonce: **rely on replay's EVM nonce enforcement**; no separate binding-layer
  check (2026-07-04).
- Fix 3: **deferred to a follow-up PR** (2026-07-04). When taken up, it owns the
  `raiko2` adapter change (full headers in `proposal_ancestor_headers`) and must
  pin the window-size contract (cover the deepest `BLOCKHASH` reach — up to 256
  below the first proven block).
