# Gaiko2 GuestInput Soundness Design

**Goal:** Raise `gaiko2` from a replay-only reference checker to a soundness-level remote prover by making the proposal input semantically equivalent to the `raiko2` Shasta `GuestInput`.

## Context

`gaiko2` currently receives a compact replay packet:

- `chain_id`
- replay blocks
- replay witnesses
- `proof_carry_data`

That packet is enough for `taiko-geth` stateless replay and checkpoint validation, but it is not enough to independently bind a Shasta proposal source to the replayed block range. In particular, it omits:

- `TaikoManifest`
- proposal event data
- proposal `data_sources`
- proposal ancestor headers and shared state pools
- derivation source metadata required to bind blobs or calldata to canonical block contents

That was acceptable while `gaiko2` was only an EVM reference checker. It is not enough if `gaiko2` should have the same soundness boundary as a `raiko2` guest.

## Decision

The proposal input source for `gaiko2` must become `GuestInput`.

The route can remain:

- `POST /prove/shasta`

But the proposal request schema must change because the payload semantics change. Use a new schema such as:

- `raiko2-shasta-request-v2`

with payload:

```json
{
  "schema": "raiko2-shasta-request-v2",
  "payload": {
    "guest_input": {
      "witnesses": [],
      "taiko": {},
      "proposal_ancestor_headers": [],
      "proposal_state_nodes": [],
      "proof_carry_data": {}
    }
  }
}
```

The old replay-only schema, `raiko2-shasta-request-v1`, should not be treated as soundness-equivalent. It can either be removed from the active service path or kept only behind a local/dev compatibility flag. Production proving should reject it after the cutover.

## Implementation Status

The current implementation accepts `raiko2-shasta-request-v2` on the existing
`POST /prove/shasta` route and validates:

- strict `GuestInput` decoding into replay-compatible witnesses,
- `proof_carry_data` binding to the proposal, prover, verifier, and final checkpoint,
- raw blob KZG commitment and versioned hash matching proposal blob hashes,
- inline calldata and blob-backed Shasta source manifest decoding,
- Shasta manifest metadata defaulting and validation for timestamp, anchor number, gas limit, forced inclusion, and source block limits,
- canonical transaction and block metadata binding before `taiko-geth` replay.

The old `raiko2-shasta-request-v1` replay-only path remains accepted for compatibility
in this branch. It should still be considered unsafe for production soundness until
it is guarded by an explicit compatibility flag or removed from production service
configuration.

## Soundness Boundary

For proposal proofs, `gaiko2` should validate the same conceptual chain as the `raiko2` guest:

```text
GuestInput
  -> proposal event and proposal sources
  -> data source blob/calldata bytes
  -> derived Shasta block manifest
  -> canonical block tx list and metadata
  -> taiko-geth stateless replay
  -> proof_carry_data
  -> signed public input
```

The service must not sign whatever replay result it computes unless it first proves that the replayed blocks are derived from the proposal data embedded in the `GuestInput`.

## Blob Verification

`gaiko2` does not need to reuse `raiko2`'s zk-oriented blob proof-of-equivalence.

For TEE/geth execution, the simpler and preferred validation is:

1. Read each raw blob from `GuestInput.taiko.data_sources`.
2. Compute the KZG commitment from the raw blob inside `gaiko2`.
3. Compute the EIP-4844 versioned hash from that computed commitment.
4. Compare the versioned hash with `GuestInput.taiko.proposal_event.proposal.sources[*].blobSlice.blobHashes[*]`.

The request may still carry commitments or proofs for compatibility, but they are untrusted. `gaiko2` must not accept a blob just because caller-supplied commitment/proof fields are internally consistent. The raw blob bytes must be the source of truth.

This differs from zk guests only because zk guests optimize for an in-circuit verification cost. The TEE path can afford the direct KZG computation and should prefer the simpler implementation.

## Manifest Binding

Blob validation alone is insufficient.

After blob or calldata source validation, `gaiko2` must decode the derivation source manifest and bind it to the replayed blocks. For each derived block:

- The number of derived manifest blocks must equal `GuestInput.witnesses.len()`.
- The canonical block must include exactly one anchor transaction plus the manifest transactions.
- Every non-anchor transaction must match the manifest transaction by canonical encoding.
- Header metadata must match the manifest and protocol rules:
  - timestamp
  - coinbase
  - gas limit plus anchor gas
  - extra data including proposal id and basefee sharing pctg
  - Shasta difficulty or mix hash value
- The anchor transaction must be bound to the expected L2 contract, chain id, nonce, fee fields, and anchor checkpoint.

This is the main missing soundness piece in the replay-only v1 input.

## Carry Binding

`proof_carry_data` must be treated as derived or validated data, not as an independent authority.

`gaiko2` must validate:

- `proof_carry_data.chain_id` equals the `GuestInput` chain id selected from the witness or Taiko chain spec.
- `transition_input.proposal_id` equals `GuestInput.taiko.proposal_id`.
- `transition_input.proposal_hash` equals the hash of `GuestInput.taiko.proposal_event.proposal`.
- `transition_input.parent_proposal_hash` equals the proposal event parent proposal hash.
- `transition_input.parent_block_hash` equals the first witness block parent hash.
- `transition_input.actual_prover` equals `GuestInput.taiko.prover_data.actual_prover`.
- `transition_input.transition.proposer` and `timestamp` equal the proposal event.
- `transition_input.checkpoint` equals the last replayed block number, block hash, and state root.
- `verifier` resolves from the witness chain spec for the `gaiko2`/TEE proof type at the relevant fork.

The final signed input remains the Shasta subproof input hash derived from `proof_carry_data`.

## Replay Binding

The current `taiko-geth` replay path should remain the execution engine:

- build a state database from the witness,
- replay each block with `taiko-geth`,
- validate state root and receipt root,
- validate post-execution request hash,
- validate Unzen zk-gas truncation behavior when active.

The difference is that replay now runs after `GuestInput` proposal binding, not as the only soundness check.

## API Shape

### Proposal Request

Use a v2 proposal request:

```go
type ShastaRequestV2 struct {
    Schema  string           `json:"schema"`
    Payload ShastaPayloadV2  `json:"payload"`
}

type ShastaPayloadV2 struct {
    GuestInput ShastaGuestInput `json:"guest_input"`
}
```

`ShastaGuestInput` can be implemented as Go structs that mirror the stable JSON emitted by `raiko2_primitives_shasta::GuestInput`.

Avoid reusing the v1 replay packet as the canonical source. A compatibility adapter may internally convert a fully validated `GuestInput` into replay blocks for the existing replay runner, but the wire input must carry the complete `GuestInput` semantics.

### Aggregate Request

The aggregate request can remain based on child proofs and `proof_carry_data`:

- `raiko2-shasta-aggregate-request-v1`

No full child `GuestInput` is required for aggregation as long as proposal proofs already validated and signed their child input hashes, and aggregate validation continues to enforce a contiguous carry sequence with consistent verifier, chain id, actual prover, proposal hash chain, and parent block hash chain.

## Reuse Strategy

Reuse existing code where it gives the right trust boundary:

- Use `taiko-geth` for KZG helpers and EVM/stateless replay.
- Use `taiko-geth` transaction, header, block, receipt, and KZG types instead of local encodings.
- Prefer extracting reusable Shasta derivation helpers from Taiko's Go client side if a stable package exists or can be added.
- If no suitable Go derivation package exists, implement the minimum Shasta derivation decoder in `gaiko2` with golden vectors generated by `raiko2`.

Do not import `raiko2` Rust internals into `gaiko2` at runtime. Cross-language parity should be enforced through fixtures and conformance tests.

## Testing Strategy

Unit tests should cover:

- v2 request schema acceptance and v1 rejection in production mode,
- `GuestInput` JSON decoding,
- blob commitment/versioned-hash validation from raw blob bytes,
- rejection when caller-supplied commitment/proof fields are consistent but raw blob bytes do not match the proposal hash,
- derived manifest transaction mismatch,
- derived manifest block metadata mismatch,
- carry mismatch for every critical carry field,
- successful conversion from validated `GuestInput` to replay blocks,
- aggregate validation unchanged.

Golden/conformance tests should cover:

- a `raiko2` generated Shasta `GuestInput` fixture with blob-backed data source,
- the same fixture through `gaiko2` proposal proving,
- the returned child input hash matching `hash_shasta_subproof_input(proof_carry_data)`,
- aggregate proof over one or more gaiko2 child proofs.

## Migration Plan

1. Add v2 request structs and decoding.
2. Add a `GuestInput` validator that performs proposal/blob/manifest/carry checks.
3. Convert validated `GuestInput` into the existing replay runner input.
4. Update the `raiko2` gaiko2 adapter to send v2 `guest_input` payloads.
5. Keep v1 only as a dev/test compatibility path if needed.
6. Update conformance fixtures and docs.
7. Remove production acceptance of replay-only v1 before treating `gaiko2` as soundness-equivalent to zk guests.

## Non-Goals

This work does not:

- require zk proof-of-equivalence in `gaiko2`,
- require `gaiko2` to fetch witnesses or blobs from RPC directly,
- replace the current `taiko-geth` replay engine,
- change aggregate proof hashing semantics,
- change TEE attestation semantics,
- create a new public external API for users.

## Open Questions

1. Should production immediately reject v1 replay-only requests, or should v1 remain behind an explicit unsafe compatibility flag for one release?
2. Should calldata-backed Shasta sources be accepted if the protocol permits them, or should `gaiko2` follow the current zk guest policy and reject inline payloads for proposal-mode proving?
3. Where should the shared Shasta derivation helper live if a reusable Go package does not already exist: `taiko-client`, `taiko-geth`, or temporarily inside `gaiko2`?
