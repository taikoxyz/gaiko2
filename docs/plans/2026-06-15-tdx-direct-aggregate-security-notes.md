# TDX Direct Aggregate Security Notes

Date: 2026-06-15

This note records the current security boundary for the TDX direct aggregate path and the checks that
must be added before treating a TDX proof as independent from the raiko2 host request.

## Current Model

TDX direct aggregate avoids producing per-proposal subproofs. The TDX service reads local L2 headers,
builds Shasta proof carry data, and signs the Shasta aggregation public input directly.

For the current trusted-prover/devnet flow, the host request is still trusted for proposal metadata:

- `proposal_hash`
- `parent_proposal_hash`
- `proposer`
- `timestamp`
- `actual_prover`
- `verifier`

This is acceptable as a temporary devnet/trusted-prover path, but it is not sufficient for a
malicious-prover security boundary.

## Issues And Mitigations

### Request-Supplied Proposal Metadata

Problem: the host can provide `proposal_hash`, `parent_proposal_hash`, proposer, timestamp, and blob
source-derived metadata. If TDX signs these fields without checking them against event-derived data,
the blob/source commitment chain is not closed inside TDX.

Current mitigation: none in gaiko2 direct aggregate. The trusted-prover path temporarily accepts this.

Required mitigation: expose a local client/engine auth RPC that returns event-derived proposal
metadata by proposal ID, then have the TDX service build carry data from that local metadata instead
of the request. Candidate shape:

- `proposal_id -> proposal_hash`
- `proposal_id -> parent_proposal_hash`
- `proposal_id -> proposer`
- `proposal_id -> timestamp`
- `proposal_id -> sources/blob hashes`
- `proposal_id -> L1 origin block hash/height`

The TDX service does not need to redo full L1 derivation if the measured client has already derived
and persisted this metadata from L1 events.

### Event-Covered Block Range

Problem: a host-supplied `l2_block_numbers` range can be incomplete or not correspond to the
proposal's event-covered range.

Mitigation added in gaiko2 direct aggregate:

- Query local engine `taikoAuth_lastCertainBlockIDByBatchID(proposal_id)`.
- Query local engine `taikoAuth_lastCertainBlockIDByBatchID(proposal_id - 1)` for proposals after
  the first real proposal.
- For proposal 1, treat genesis proposal 0 as ending at block 0.
- Require request first block to equal previous proposal last block plus one.
- Require request last block to equal current proposal's certain last block.
- Query local engine `taikoAuth_lastCertainL1OriginByBatchID(proposal_id)`.
- Require the certain L1 origin block ID and L2 block hash to match the local last header.
- Require the certain L1 origin to include L1 block height and L1 block hash.

These checks make direct aggregate fail when the local client/engine has not yet derived a certain
event-covered proposal range.

### Local Header Consistency

Problem: local headers can be inconsistent with the requested range or proposal ID.

Current mitigation:

- Fetch every requested local L2 header by number.
- Require header number to match the requested number.
- Require each header to contain a Shasta proposal ID in `extraData`.
- Require each header proposal ID to equal the current proposal ID.
- Require parent hash continuity within the requested range.
- Keep left/right boundary checks as defense-in-depth:
  - previous block must belong to the previous proposal;
  - next block must belong to the next proposal.

### Blob Hash Chain

Blob hashes are part of `Proposal.sources`, and `hashProposal(proposal)` commits to them through
`keccak256(abi.encode(proposal))`. The final Shasta commitment only contains `lastProposalHash`, so
blob hashes are included indirectly through the proposal hash chain.

Risk: if TDX accepts proposal hashes from the host, the blob/source chain is not independently
verified inside TDX.

Required mitigation: the proposal metadata RPC above must return event-derived proposal data, or TDX
must query L1/InBox itself and recompute proposal hashes.

### L1 RPC Trust

If TDX queries L1 directly, L1 RPC becomes part of the trust boundary. A host-controlled fake L1 RPC
could feed fake events unless TDX pins enough chain context.

Todo for production:

- Pin chain ID and Inbox address in the measured image or config spec.
- Verify L1 block hash/finality window.
- Consider multiple RPCs, a trusted checkpoint, or a light-client-style source for stronger trust.

This is not the first devnet priority if proposal metadata comes from the local measured client.

### Storage Integrity

TDX does not automatically protect disk state from rollback or replacement by the host. A stale but
self-consistent DB can still have internally consistent block hashes and state roots.

Mitigation status:

- Event-covered range checks reduce the chance of signing arbitrary stale ranges.
- Production should still define a sync and storage policy: from genesis, from a trusted checkpoint,
  sealed DB metadata, dm-verity, or no untrusted snapshots.

### Client Bugs

This path relies on the measured client/engine correctly deriving L2 blocks from L1 proposal data.
Client consensus bugs are out of scope for the malicious-prover threat model in this note.

### P2P And Preconf

If TDX only signs event-covered proposal/block ranges returned by the local certain mapping, P2P and
preconf data should not directly allow invalid proofs. They can still affect liveness, sync behavior,
and debugging complexity.

Production image recommendation:

- Disable external P2P ingress unless explicitly needed.
- Disable preconf proof generation for this TDX profile unless there is a separate preconf security
  design.
- Prefer an L1-event-driven client profile for proof production.

## SGX/ZK Comparison

ZK and SGX guest paths validate proposal metadata inside the guest flow:

- carry `proposal_hash` is checked against `hash_proposal(GuestInput.taiko.proposal_event.proposal)`;
- parent proposal hash, proposer, and timestamp are checked against `GuestInput`;
- block derivation and stateless validation run in the guest path.

TDX direct aggregate bypasses that `GuestInput` validation path. Therefore it needs equivalent
TDX-local checks against the measured client/engine state before signing.

## Near-Term Plan

1. Keep trusted-prover/devnet behavior for proposal metadata.
2. Add event-covered checks using local `taikoAuth_lastCertain*` RPCs.
3. Discuss with client/engine owners whether proposal metadata is already persisted.
4. If not persisted, add a local auth RPC for proposal metadata by proposal ID.
5. Change TDX direct aggregate to construct proposal metadata from local client/engine state, not from
   the host request.
