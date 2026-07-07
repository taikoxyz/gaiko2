# Gaiko2 Anchor Canonicity Soundness Design

**Goal:** Close two verified proof-soundness gaps in `gaiko2`'s Shasta manifest
binding, both of which let a caller who can deliver a crafted `guest_input`
obtain a registered-TEE signature over a **non-canonical L2 block hash** that
`Inbox.prove` then finalizes on L1 with no on-chain recourse.

## Context

`gaiko2` is a soundness-level remote prover. Per
`docs/plans/2026-06-29-gaiko2-guestinput-soundness-design.md` it must treat the
`GuestInput` as **untrusted** and independently prove that the replayed blocks
are the canonical derivation of the proposal before signing (that doc, lines 87
and 100). The two gaps below are places where manifest binding trusts
request-supplied bytes/addresses instead of reconstructing the canonical value.

Both were traced end-to-end and confirmed against the code, the reference driver
(`taiko-client-rs`), and the contracts:

- On-chain there is no re-derivation or dispute. `SgxVerifier.verifyProof` only
  checks a registered-instance ECDSA signature over the commitment hash
  (`SgxVerifier.sol:467-479`), and `Inbox.sol:350-383` writes
  `commitment.transitions[last].blockHash` straight into `lastFinalizedBlockHash`
  and the SignalService checkpoint. The enclave's internal validation is the
  only soundness gate.
- The signed value carries the block hash: `validate.go:158` binds the last
  replayed block hash to `carry.Checkpoint.BlockHash`, which becomes
  `shastaTransition.BlockHash` in `hashCommitment` (`hash.go:131`).

### Shared root cause

Manifest binding accepts a first-transaction / parent-checkpoint that decodes to
the right *semantic* fields but is not the *exact canonical* value the reference
derivation would produce. The reference driver rejects any block whose first
transaction is not byte-for-byte the derived anchor transaction
(`taiko-client-rs .../driver/src/derivation/pipeline/shasta/pipeline/payload.rs:743`).
`gaiko2` must match that strictness.

## Fix A — Enforce canonical anchor calldata (Finding: trailing anchorV4 calldata)

### Problem

`decodeAnchorV4Checkpoint` (`internal/prover/manifest_validate.go:1046-1063`)
guards only `len(input) < 4+96` and reads the decoded fields; trailing bytes are
ignored. `validateManifestAnchorTransaction` (`:958-1029`) checks anchor envelope
fields and the decoded checkpoint but never the calldata length or canonical
bytes. The transaction-root check reuses the block's own first transaction
(`manifest_tx_filter.go:104`, `:37`), so trailing bytes are self-consistent with
the header and pass. Solidity's `anchorV4((uint48,bytes32,bytes32))` tolerates
trailing calldata (fully static param, no `msg.data.length` check —
`Anchor.sol:124`), so replay also passes. Result: appending bytes to the anchor
calldata yields a different tx-root → different block hash → signed and
finalized, while no honest node derives that hash.

### Change

In the anchor-decode path, require the calldata to be **exactly** the canonical
encoding of the decoded checkpoint — byte-for-byte equal to
`selector ‖ pad32(blockNumber) ‖ blockHash ‖ stateRoot`. Concretely, in
`decodeAnchorV4Checkpoint`:

- reject unless `len(input) == 4+96` (kills trailing bytes);
- reject unless the high 24 bytes of the `blockNumber` word (`input[4:4+24]`) are
  zero (canonical `uint48` left-padding);
- keep the existing `blockNumber > maxUint48` guard and field extraction.

These guards together make `tx.Data()` identical to the re-encoding of the
decoded `(blockNumber, blockHash, stateRoot)`; an equivalent implementation is to
re-encode and `bytes.Equal`. Either form is acceptable; the guard form is the
minimal diff at the single decoder site.

### Rationale for the padding guard (not just a length check)

The decoder reads `blockNumber` from `input[4+24:4+32]` and otherwise ignores the
first word's high bytes. On-chain those dirty bits would revert on `uint48`
cleanup, but manifest binding runs **before** replay, and `gaiko2` deliberately
keeps manifest binding sound on its own rather than leaning on later replay
checks (design value stated at `internal/prover/l2_state.go:26-31`). The padding
guard closes the malleability at binding time.

### Tests

- Negative: canonical anchor + one trailing byte → `ValidateGuestInputManifestBinding` rejects.
- Negative: canonical anchor with a non-zero high byte in the `blockNumber` word → rejects.
- Positive: exact 100-byte anchor still binds; the checked-in mainnet fixture
  (`testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`)
  and `internal/prover/replay_fixture_test.go` stay green.

## Fix B — Pin the CheckpointStore address (Finding: request-selected CheckpointStore)

### Problem

`decodeWitnessCheckpointStore` (`internal/prover/manifest_validate.go:1122-1131`)
reads the parent checkpoint-store address from request-controlled
`witness.chain_spec.checkpoint_store_contract` with no pinning.
`verifiedParentShastaCheckpoint` (`:1099-1120`) then reads the parent checkpoint
from that address in parent L2 state. The MPT read is bound to the parent state
root (`l2_state.go:32`, `:65`) but the **address** is untrusted: an attacker who
prepares L2 storage before the parent block (permissionless L2) can deploy a
contract at an address they control, seed the checkpoint slots, set
`checkpoint_store_contract` to it, and feed `gaiko2` forged parent-checkpoint
values. This path is reached on the stalled-anchor bypass (`:1217`) and the
forced-inclusion prefix (`:1234`). Because those are the stale/equal cases,
`Anchor.sol:181` does not save the checkpoint, so replay state is unchanged — but
the forged values still flow into the anchor calldata → non-canonical block hash.
(The normal path is already sound: it binds checkpoints to real L1 ancestor
headers chained to `proposal.OriginBlockHash`, `:1246-1294`.)

### Change

Derive the checkpoint-store address deterministically from the chain-id instead
of reading it from the request. The L2 SignalService that holds checkpoints at
slot 254 is a deterministic predeploy at `0x{chainid}…0005` (mainnet 167000 →
`0x1670000000000000000000000000000000000005`), the same predeploy scheme
`gaiko2` already uses for the TaikoL2/Anchor address at `…10001`
(`manifest_validate.go:1031-1037`, confirmed against
`protocol/contracts/layer2/mainnet/LibL2Addrs.sol`).

- Generalize `shastaTaikoL2Address` into a shared helper
  `shastaL2PredeployAddress(chainID, suffix)`; add
  `shastaSignalServiceAddress(chainID)` using suffix `"5"`. Keep
  `shastaTaikoL2Address` as a thin wrapper (suffix `"10001"`).
- In `verifiedParentShastaCheckpoint`, obtain the store via
  `shastaSignalServiceAddress(view.GuestInputChainID)`.
- Witness field handling: **derive-and-reject-on-mismatch**. If
  `witness.chain_spec.checkpoint_store_contract` is present and differs from the
  derived address, reject (surfaces upstream tampering/misconfig). The derived
  address is always the one used. `decodeWitnessCheckpointStore` changes from
  "require and use" to "optionally read and compare".

Deriving (not reading from Anchor state) is required because
`Anchor.checkpointStore` is `immutable` (`Anchor.sol:47`) — it lives in bytecode,
not a readable storage slot. The slot-254 storage-slot computation
(`shastaCheckpointStorageSlots`) is unchanged; only the account address changes.

### Tests

- Negative: an attacker-chosen store address with matching seeded storage on a
  stalled-anchor (or forced-inclusion) fixture → rejected.
- Negative: `checkpoint_store_contract` present in the witness but disagreeing
  with the derived address → rejected.
- Positive: derived SignalService address with the canonical parent checkpoint →
  accepted.
- Unit: `shastaSignalServiceAddress(167000) == 0x1670000000000000000000000000000000000005`.

## Non-Goals / Scope boundaries

- `alethia-reth`'s parallel selector-only anchor validation
  (`crates/consensus/src/validation/anchor.rs`) is a real, analogous gap but
  lives in a different repository; flag it, do not fix it here.
- Other `witness.chain_spec`-sourced values (fork timestamps in
  `witnessForkActiveAt`/`decodeWitnessForkTimestamp`, verifier resolution) share
  Finding B's "trust the chain spec" smell and deserve a separate audit; not
  folded in, to keep this change focused.
- Endpoint authentication and the legacy `raiko2-shasta-request-v1` schema are
  deployment/design concerns tracked elsewhere.
- No change to replay, aggregate hashing, TEE attestation, or the wire schema.

## Acceptance

- Both negative tests fail before the change and pass after.
- The mainnet replay fixture and full `go test ./...` for `internal/prover` stay
  green (pre-existing `internal/tee` permission-assertion failures are unrelated).
