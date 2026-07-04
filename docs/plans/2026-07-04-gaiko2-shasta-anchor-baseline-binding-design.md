# Gaiko2 Shasta Anchor Baseline Binding Design

**Goal:** Make the Shasta anchor-progression baseline come from authenticated parent
L2 `Anchor._blockState.anchorBlockNumber` state instead of the caller-supplied
`taiko.prover_data.last_anchor_block_number` field, so a crafted request cannot make
`gaiko2` sign a non-canonical Shasta block.

## Context

Shasta manifest validation checks that each derived block's anchor block number advances
correctly from the parent block's anchor baseline. Today `gaiko2` seeds that baseline
directly from a caller-supplied `GuestInput` field:

- `internal/prover/manifest_validate.go:110` — `lastAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)`
- `internal/prover/manifest_validate.go:1151` — `decodeGuestInputLastAnchorBlockNumber` reads `taiko.prover_data.last_anchor_block_number` and returns it verbatim (defaulting to `0` when absent).

That `lastAnchor` value is the authoritative baseline for every anchor-progression check:

- `:117` — seeds the parent block's `AnchorBlockNumber` for derivation.
- `:604` — in-range anchors must equal it (`validateAnchorLinkage`).
- `:651` — `anchor < lastAnchor` is the progression floor (`validateManifestAnchorProgression`).
- `:1206` — the stalled-anchor bypass path keys the parent checkpoint read off it.

Nothing binds `lastAnchor` to the real parent `Anchor._blockState.anchorBlockNumber`.
Surrounding checks authenticate the parent block hash, L1 checkpoint data, anchor
transaction shape, and replay roots, but none proves the baseline itself.

## The Vulnerability

Because the baseline is prover-supplied, a crafted request can advertise a stale baseline
(e.g. `last_anchor_block_number: 899`) while the real parent anchor state is already `900`.
A normal source that anchors at `900` then appears to "advance" from `899` even though it
does not advance from the real parent baseline that `taiko-client-rs` would use. If the
legitimate proof service signs that input, the signed proof can drive Inbox finalization
for a block hash / state root that does not match canonical Shasta derivation.

This is confirmed by focused overlay testing: current `gaiko2` accepts a normal-source
anchor `900` when `last_anchor_block_number` is forged to `899`, while the same
progression check rejects the source when the real parent baseline is `900`.

### Why the naive fix is not enough

Reading the anchor state from a caller-supplied contract address would only relocate the
forgery. In the threat model the attacker controls the entire `GuestInput`, including the
witness `chain_spec`. An attacker can deploy a helper contract at any L2 address, set its
storage slot `256` to an arbitrary value, and supply a valid Merkle proof of that value
against the real parent state root. So the anchor contract address must itself be trusted,
not read from the witness `chain_spec`.

`raiko2` handles this by reading `l2_contract` from `chain_spec` and separately
authenticating it via `validate_known_chain_spec` against a baked-in address table.
`gaiko2` does **not** have such a table, but it already has an equivalent trusted resolver.

## Reference Parity (raiko2)

`raiko2` derives the baseline in `crates/guest-common/src/lib.rs`:

- `verified_parent_anchor_block_number` reads slot `ANCHOR_BLOCK_STATE_SLOT` (`256`) from
  the L2 anchor contract in proof-backed parent state and uses that as the baseline.
- `prover_data.last_anchor_block_number` is treated as an **optional equality
  cross-check**: if present it must equal the verified value, otherwise the verified value
  is used unconditionally.
- `anchor_block_number_from_storage_word` extracts a `uint48` from the least-significant
  48 bits of the storage word.

## Decision

Adopt raiko2's semantics, adapted to gaiko2's architecture:

1. Derive `verifiedAnchor` from proof-backed parent Anchor state, reading slot `256` from
   the **already-trusted** `shastaTaikoL2Address(chainID)` address rather than from the
   untrusted witness `chain_spec`.
2. Use `verifiedAnchor` as the authoritative baseline for all anchor-progression checks.
3. If `taiko.prover_data.last_anchor_block_number` is present, require it to equal
   `verifiedAnchor`; if absent, use `verifiedAnchor` unconditionally.

gaiko2 already provides both required primitives:

- `readParentL2Storage(view, account, slot)` (`internal/prover/l2_state.go:14`) — a
  proof-backed storage read bound to the committed parent block hash
  (`view.Carry.TransitionInput.ParentBlockHash`). Already used for the checkpoint store.
- `shastaTaikoL2Address(chainID)` (`internal/prover/manifest_validate.go:1017`) — a
  trusted, baked-in anchor address deterministically derived from the chain id, already
  used to authenticate the anchor-transaction recipient.

Because `shastaTaikoL2Address` is trusted (not read from `chain_spec`), no new
chain-spec-authentication subsystem is required, and the forgery cannot relocate to a
caller-controlled `l2_contract`.

## Components

All changes are in `internal/prover/manifest_validate.go` unless noted.

### New

- `const shastaAnchorBlockStateSlot uint64 = 256` — the `Anchor._blockState` base slot,
  beside the existing `shastaSignalServiceCheckpointsSlot uint64 = 254`. Matches raiko2's
  `ANCHOR_BLOCK_STATE_SLOT`.

- `anchorBlockNumberFromStorageWord(word common.Hash) uint64` — extracts the `uint48`
  from the least-significant 48 bits (`word[26:32]`, big-endian), mirroring raiko2's
  `anchor_block_number_from_storage_word`. Higher-order bytes (other packed struct fields)
  are ignored.

- `verifiedParentAnchorBlockNumber(view *GuestInputView) (uint64, error)`:

  ```go
  l2Addr, err := shastaTaikoL2Address(view.GuestInputChainID) // trusted, baked-in
  if err != nil {
      return 0, fmt.Errorf("derive TaikoL2 address for parent anchor state: %w", err)
  }
  slot := common.BigToHash(new(big.Int).SetUint64(shastaAnchorBlockStateSlot))
  word, err := readParentL2Storage(view, l2Addr, slot) // proof-backed
  if err != nil {
      return 0, fmt.Errorf("read parent Anchor._blockState.anchorBlockNumber: %w", err)
  }
  return anchorBlockNumberFromStorageWord(word), nil
  ```

### Changed

- `decodeGuestInputLastAnchorBlockNumber` returns `(*uint64, error)` instead of
  `(uint64, error)`, so an absent field (`nil`) is distinguishable from a present zero.
  This is required for the optional equality cross-check.

- `internal/prover/manifest_validate.go:110` — replace the single decode line with:

  ```go
  verifiedAnchor, err := verifiedParentAnchorBlockNumber(view)
  if err != nil {
      return err
  }
  hostAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
  if err != nil {
      return err
  }
  if hostAnchor != nil && *hostAnchor != verifiedAnchor {
      return fmt.Errorf(
          "prover_data.last_anchor_block_number mismatch: expected %d (parent Anchor state), got %d",
          verifiedAnchor, *hostAnchor)
  }
  lastAnchor := verifiedAnchor
  ```

  Everything downstream (`:117`, `:183`, `:211`) is untouched; it now receives the
  authenticated baseline.

## Trust Chain

```text
view.GuestInputChainID          ← cross-checked vs proof_carry_data.chain_id (on-chain anchored)
  → shastaTaikoL2Address(chainID)                 [TRUSTED baked-in address, not from chain_spec]
  → readParentL2Storage(view, l2Addr, slot 256)   [proof against committed ParentBlockHash's stateRoot]
  → anchorBlockNumberFromStorageWord (low 48 bits)
  → verifiedAnchor  ── authoritative baseline for all progression checks
       └─ if prover_data.last_anchor_block_number present → require equality, else reject
```

No forgery survives: the address is trusted and the value is proven against the real parent
state root.

## Error Handling

`verifiedParentAnchorBlockNumber` fails closed. `readParentL2Storage` already surfaces
missing or incomplete witness-node errors explicitly
(`internal/prover/l2_state.go:61-69`) rather than returning a zero slot, so an incomplete
or corrupt witness cannot masquerade as a legitimately empty anchor slot. If the anchor
slot-`256` node is absent, validation rejects rather than silently treating the baseline
as `0`.

## Testing Strategy

Test-first (red → green). The harness already seeds parent L2 storage that flows through
`readParentL2Storage` using `statedb.SetState(account, slot, value)` and
`view.Raw.ProposalStateNodes` (see `TestReadParentL2StorageReturnsCheckpoint`,
`internal/prover/manifest_validate_test.go:1775`, and the state-trie builder around
`:1920`). Seeding the anchor contract's slot `256` reuses that exact pattern.

### Shared fixture change (prerequisite)

The shared `manifestBindingFixture` builder currently bakes
`prover_data.last_anchor_block_number: 899` into every view it produces
(`internal/prover/manifest_validate_test.go:949`) and seeds **no** anchor slot-`256`
state. After the fix, every test using this builder would invoke
`verifiedParentAnchorBlockNumber`, read slot `256` from `shastaTaikoL2Address(chainID)`,
and fail closed (missing witness node). So the builder must be extended to:

- seed the `shastaTaikoL2Address(chainID)` account's slot `256` with a configurable
  baseline, **defaulting to `899`** so the value matches the existing field and all current
  positive tests stay green, and
- make that slot value overridable per-test (so the exploit test can diverge it from the
  field).

This shared change is the bulk of the test-side work.

### New tests

1. **Feasibility / positive on real wire.** Exercise the real
   `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json` fixture
   (already loaded by `internal/prover/replay_fixture_test.go:17`) through
   `verifiedParentAnchorBlockNumber` / full manifest binding, asserting the read succeeds
   and equals the fixture's genuine baseline. This proves the slot-`256` node is present in
   real host output. If it fails, we have discovered a host-side witness-population gap
   immediately.

2. **Exploit regression (the PoC).** Seed anchor slot `256` = `900`, forge
   `last_anchor_block_number: 899`, assert validation now rejects with the mismatch error.
   This is the report's overlay scenario made permanent.

3. **Absent field.** Omit `last_anchor_block_number` → uses the verified value, passes.

4. **Present-and-equal.** `last_anchor_block_number` equals seeded slot `256` → passes
   (confirms the cross-check is not over-strict).

## Scope

### In scope

- The anchor-baseline binding, the optional equality cross-check, and the tests above.

### Non-goals / follow-up

- `verifiedParentShastaCheckpoint` (`internal/prover/manifest_validate.go:1085`) reads its
  address from the untrusted `checkpoint_store_contract` `chain_spec` field
  (`decodeWitnessCheckpointStore`, `:1108`) rather than a trusted resolver. This is the
  same *class* of concern on a different code path, and its values are cross-checked
  against L1 ancestor headers. It should be evaluated and, if needed, fixed as a separate
  change rather than widening this one.

- Introducing a general `validate_known_chain_spec` equivalent for gaiko2. Not required for
  this fix because the anchor address is obtained from a trusted resolver; may still be
  worthwhile independently for full raiko2 field-parity.
