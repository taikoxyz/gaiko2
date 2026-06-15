# TDX Direct Aggregate Security Notes

Date: 2026-06-15

This note records the current security boundary for the TDX direct aggregate path and the checks that
must be added before treating a TDX proof as independent from the raiko2 host request.

## Current Model

TDX direct aggregate avoids producing per-proposal subproofs. The TDX service reads local L2 headers,
builds Shasta proof carry data, and signs the Shasta aggregation public input directly.

The host request still carries proposal metadata for schema compatibility:

- `proposal_hash`
- `parent_proposal_hash`
- `proposer`
- `timestamp`
- `actual_prover`
- `verifier`

`tdxgeth` no longer uses the request-supplied proposal hash, parent proposal hash, proposer, or
timestamp when building the signed direct aggregate carry data. It queries the measured local
taiko-client proposal API by proposal ID and combines that event-derived metadata with local L2
headers before signing.

## Issues And Mitigations

### Request-Supplied Proposal Metadata

Problem: if TDX signs host-supplied `proposal_hash`, `parent_proposal_hash`, proposer, timestamp, and
blob source-derived metadata without checking them against event-derived data, the blob/source
commitment chain is not closed inside TDX.

Current mitigation: gaiko2 direct aggregate queries the local taiko-client proposal API:

- `GET /internal/shasta/proposals/{proposal_id}`

The response supplies the event-derived proposal hash, parent proposal hash, proposer, and timestamp.
The endpoint must be loopback and served by the measured taiko-client inside the TDX VM.

Remaining boundary: taiko-client still derives this metadata from its configured L1 RPC. Production
must define the L1 RPC trust/freshness policy separately.

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

Current mitigation: TDX direct aggregate ignores host-supplied proposal hashes and uses the measured
local taiko-client proposal API. The proposal API computes `hashProposal` from event-derived proposal
data.

### L1 RPC Trust

If TDX queries L1 directly, L1 RPC becomes part of the trust boundary. A host-controlled fake L1 RPC
could feed fake events unless TDX pins enough chain context.

Todo for production:

- Pin chain ID and Inbox address in the measured image or config spec.
- Verify L1 block hash/finality window.
- Consider multiple RPCs, a trusted checkpoint, or a light-client-style source for stronger trust.

This remains a production security boundary even when proposal metadata comes from the local measured
client.

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

1. Keep event-covered checks using local `taikoAuth_lastCertain*` RPCs.
2. Require local taiko-client proposal API in the measured TDX image.
3. Keep request proposal metadata only as compatibility fields; do not use it for signed carry data.
4. Define production L1 RPC trust/freshness policy.
5. Define production storage rollback and sync policy.
