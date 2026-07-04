# Gaiko2 Disable Compact Replay Ancestors Design

**Goal:** Close a soundness hole in `gaiko2` where a malicious replay packet can spoof
historical `BLOCKHASH` values. Make `gaiko2` refuse hash-only ("compact") replay
ancestors and authenticate every ancestor hash by keccak recomputation, matching the
`raiko2` reference guest and staying aligned with the fixed `raiko2` producer (PR #136).

## Context

`gaiko2` replays L2 blocks statelessly with `taiko-geth`. To serve the EVM `BLOCKHASH`
opcode (which can look back up to 256 blocks), it builds a chain context from a replay
witness. The witness carries one full parent header plus a list of **compact ancestors**
— objects with only `{number, hash, parentHash, timestamp}` and no full header:

```go
// internal/prover/types.go
type ReplayWitness struct {
    Witness          *stateless.Witness
    CompactAncestors []CompactAncestor
}

type CompactAncestor struct {
    Number     uint64
    Hash       common.Hash
    ParentHash common.Hash
    Timestamp  uint64
}
```

### Root cause

1. `decodeWitnessHeaders` ([internal/prover/decode.go:439](../../internal/prover/decode.go))
   accepts a compact ancestor and takes its `hash` field **verbatim from the wire**.
2. `validateReplayWitness` ([internal/prover/replay.go:347](../../internal/prover/replay.go))
   checks **only chain adjacency** for compact ancestors — sequential numbers,
   `current.ParentHash == prev.Hash`, and a single tail link `parent.ParentHash == last.Hash`.
   It never recomputes `keccak256(header)` to prove `Hash` is real (it cannot — the full
   header is not present).
3. `newReplayChainContext` ([internal/prover/replay.go:501](../../internal/prover/replay.go))
   stores each synthesized ancestor **keyed by the attacker-supplied `ancestor.Hash`**,
   with a stub header carrying only `{ParentHash, Number, Time}`.
4. In `taiko-geth`, `opBlockhash` resolves via `evm.Context.GetHash` → the header-walk
   `GetHashFn`, **not** EIP-2935 history-contract state, on every fork. So `BLOCKHASH`
   walks these stub headers' `ParentHash` fields.

Only two blockhashes are authenticated:

- `BLOCKHASH(N-1)` = `block.ParentHash` (committed in the block header).
- `BLOCKHASH(N-2)` = the real parent header's `ParentHash` (the parent full header is
  bound by `parent.Hash() == block.ParentHash`).

Everything from `BLOCKHASH(N-3)` down to `BLOCKHASH(N-256)` is attacker-chosen: the walk
returns compact ancestors' self-declared `ParentHash` values, constrained only to be
internally consistent with each other. A single fabricated compact ancestor is enough.

Both EVM execution paths are affected, because both build the chain context from the same
witness:

- `ReplayService.Prove` (state-transition replay).
- Manifest transaction filtering (`commitFilteredManifestTransactions`,
  [internal/prover/manifest_tx_filter.go:154](../../internal/prover/manifest_tx_filter.go)).

### Impact

A block containing any transaction that reads a deep `BLOCKHASH` executes to an
attacker-chosen state. The TEE then signs that state as correct, so a fraudulent state
transition can obtain a valid proof and finalize. This is a soundness break; severity High.

### Cross-component alignment

- **`raiko2`** (the reference Rust guest) already refuses this shape. `ensure_full_ancestor_headers`
  returns `CompactAncestorHeaderUnsupported`, and full headers always have their hash
  recomputed via `hash_slow()` (deserialization discards any host-supplied hash), on top
  of an adjacency check. `gaiko2` does only the adjacency check.
- **`raiko2` PR #136** ("fix(remote-prover): send full replay ancestor headers", merged
  2026-07-04) is the producer-side counterpart. It deleted `remote_prover_witness_headers()`
  (which ran `compact_in_place()` on all-but-the-parent ancestor) and now fails closed if
  any replay-witness header is not full. `gaiko2` is the **consumer** and is still open:
  even with `raiko2` fixed, any packet crafted with compact ancestors is accepted and
  trusted. `gaiko2` needs the mirror-image consumer-side gate.
- **The driver (`taiko-client-rs`)** builds no guest input; it drives `alethia-reth` over
  the Engine API. The compact form is produced solely by the `raiko2` adapter. "Align with
  the driver" therefore reduces to: the proven `BLOCKHASH` must equal the real chain's,
  which requires authentic (full) ancestors.

### Wire ordering

The real mainnet fixture (proposal 2222) shows the producer's replay-witness header
layout: **256 headers per witness, ordered oldest-first (ascending), parent last**. Today
exactly one is full (the parent, at the tail) and 255 are compact. `gaiko2` currently
works only because it cherry-picks the single full header as `Witness.Headers[0]`
(`taiko-geth` requires `Headers[0] = parent`; `Witness.Root() == Headers[0].Root`).

Once the producer sends all headers full (post-#136, still parent-last), naively rejecting
compact would leave the oldest header at `Headers[0]`, breaking `Root()` and the parent
checks. So the fix must both reject compact **and** re-anchor the parent to index 0.

`MakeHashDB` writes every header by its real hash and the `BLOCKHASH` walk resolves by
`GetHeader(hash, number)`, so reordering `Witness.Headers` is safe; only `Root()` depends
on `Headers[0]`.

## Decision

Full refactor of the replay-witness ancestor path to mirror `raiko2`: reject compact
replay ancestors, carry all ancestors as full headers, drop `ReplayWitness.CompactAncestors`,
re-anchor the parent to `Headers[0]`, and authenticate ancestor linkage with recomputed
keccak hashes.

The `CompactAncestor` **type** is retained; it is still used, safely, by the manifest
base-fee path via `compactAncestorFromHeader` (which derives `Hash` from a full header).
Only the untrusted replay-witness list is removed.

## Detailed Changes

### 1. Data model (`internal/prover/types.go`)

Remove the untrusted list from `ReplayWitness`:

```go
type ReplayWitness struct {
    Witness *stateless.Witness // Headers = [parent, N-2, N-3, ...] (parent-first), all full
}
```

Keep the `CompactAncestor` type as-is (manifest usage).

### 2. Decoder (`internal/prover/decode.go`)

`decodeWitness` and `decodeWitnessHeaders`:

- Fail closed on any compact header. If a witness header lacks a full `header` field (the
  hash-only shape), return an error, e.g. `compact replay ancestors are not accepted:
  witness header %d must be a full header`. This is the security gate mirroring `raiko2`'s
  `ensure_full_ancestor_headers` and the `raiko2` adapter check.
- Decode all entries as full headers.
- Reorder **parent-first** so `taiko-geth`'s `Root()`/`Headers[0] = parent` invariant
  holds. The producer emits ascending/parent-last, so this is a reverse. Reordering must
  happen in the decoder (not in `validateReplayWitness`) because the manifest-filter path
  consumes the witness without calling `validateReplayWitness`.
- Return `([]*types.Header, error)` (full headers, parent-first). The `CompactAncestor`
  return value and the compact-decoding branch are removed.

`decodeWitness` sets `Witness.Headers` from the returned slice; `ReplayWitness` no longer
has a `CompactAncestors` field to populate.

### 3. Validation (`internal/prover/replay.go`, `validateReplayWitness`)

- Keep the existing parent binding: `parent := Headers[0]`; `parent.Number + 1 ==
  block.NumberU64()`; `parent.Hash() == block.ParentHash`; `Witness.Root() == parent.Root`.
- Replace the compact-adjacency block with full-header adjacency over `Headers[1:]`:
  for each `i` in `1..len(Headers)-1`, require
  `Headers[i-1].ParentHash == Headers[i].Hash()` (recomputed keccak) and
  `Headers[i-1].Number == Headers[i].Number + 1`.

Because the ancestor hash is now recomputed, a spoofed ancestor fails here instead of
being trusted. This mirrors `raiko2`'s `compute_ancestor_hashes_for_child` adjacency check
combined with full-header hash recomputation.

### 4. Chain context (`internal/prover/replay.go`, `newReplayChainContext`)

Remove the compact-ancestor loop. Iterate `witness.Witness.Headers` and add each by its
real `header.Hash()` (already the code path for full headers). Adjust map size hints to
drop the `CompactAncestors` term.

## Testing Strategy

Synthetic unit tests are added now; the large mainnet fixture is regenerated later (see
Follow-up).

### New synthetic unit tests

- Reject a witness containing a compact (hash-only) ancestor entry at decode.
- Reject a full-ancestor chain whose recomputed hash breaks linkage
  (`Headers[i].Hash() != Headers[i-1].ParentHash`) — this is the old spoof, now caught.
- Accept a well-formed full-ancestor chain (parent + one or more full ancestors).
- `newReplayChainContext` / `GetHash` returns the real ancestor hash at `N-3` for a
  full-ancestor witness (direct `GetHashFn` behavior, avoiding a full EVM run).

### Existing tests

- Single-full-header unit tests in `internal/prover/replay_test.go` continue to pass (one
  full header = parent at `Headers[0]`, no ancestors).
- The two big-fixture tests carry compact ancestors and will fail once compact is rejected.
  They are guarded with `t.Skip` and a TODO that references fixture regeneration:
  - `internal/prover/replay_fixture_test.go`
  - `internal/api/server_test.go`

## Follow-up (deferred)

Regenerate `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`
from `raiko2` `origin/main`'s `dump_gaiko2_shasta_fixture` example (all-full ancestor
headers, minified JSON) and un-skip the fixture tests. Tracked as a separate task; it
depends on building `raiko2` and produces a larger fixture.

## Non-Goals

This work does not:

- change the manifest proposal-ancestor path (already full-header-only for parent and
  grandparent; grandparent hash is derived from a full header),
- add an EIP-2935 history-contract `BLOCKHASH` path (`taiko-geth`'s `opBlockhash` does not
  use it),
- change the `raiko2` producer (already fixed in PR #136),
- change TEE attestation, aggregate hashing, or the public API,
- reduce the number of ancestors the producer sends (packet-size tradeoff is a
  producer-side decision already accepted by `raiko2`).

## Risks

- **Temporary coverage gap.** Skipping the fixture tests removes full mainnet replay
  coverage until regeneration. Mitigated by the synthetic tests and the follow-up task.
- **Ordering assumption.** The decoder assumes the producer's parent-last ordering when it
  reverses. This is backstopped by the fail-closed contiguity check in validation: any
  deviation (wrong order, gap, or bad hash) errors instead of mis-selecting the parent.
- **Packet size.** All-full headers enlarge packets (256 per witness). This is accepted on
  the `raiko2` side (minified dump) and requires no `gaiko2` action beyond decoding.

## Open Questions

None outstanding. The producer contract (full, ascending, parent-last), the `taiko-geth`
`BLOCKHASH` mechanics, and the `raiko2` reference model are all confirmed.
