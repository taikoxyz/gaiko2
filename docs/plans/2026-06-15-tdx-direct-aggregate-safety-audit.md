# TDX Direct Aggregate Safety Audit Process

Date: 2026-06-15

This document describes how to audit the TDX direct aggregate proving path after
gaiko2 started deriving proposal metadata from the local taiko-client proposal API.

The purpose is narrow: under the stated TDX model, a malicious remote caller or
host request should not be able to make the TDX service sign a fake proof that is
accepted by the real Taiko L1 contracts.

## Audited Path

The audited path is `tdxgeth` direct aggregate:

```text
remote caller
  -> gaiko2 POST /prove/shasta-direct-aggregate
  -> local taiko-client proposal API
  -> local L2 engine/auth RPC
  -> TDX signer
  -> Shasta aggregate public input
  -> on-chain Inbox.prove
  -> configured proof verifier over commitmentHash
```

The service does not build per-proposal proposal proofs first. It builds the
Shasta carry vector directly from local proposal metadata and local L2 headers,
then signs the same aggregate public input shape used by the normal aggregate
path.

## Security Goal

The safety goal is:

- If the TDX service returns a proof that the real L1 verifier accepts, the proof
  must correspond to real L1 proposal metadata and the local measured node's
  derived L2 transition for that proposal range.
- If the caller, host request, L1 RPC view, local sync state, or storage state is
  inconsistent with the measured client/node view, the TDX service must fail or
  produce a proof that the real L1 rejects.

The following are not safety failures for this document:

- no proof is produced;
- an invalid proof is produced and rejected by L1;
- the TDX node is out of sync;
- the TDX deployment is not yet hardened against SSH or operator access;
- the measured taiko-client or node implementation itself has a consensus bug.

## Threat Model

Trusted components:

- The measured TDX image and the binaries/config it contains.
- The measured local taiko-client, local L2 node, gaiko2 direct aggregate code,
  and TDX signer.
- The real Taiko L1 contracts, especially `Inbox.prove`, commitment hashing, and
  the configured proof verifier.
- The accepted TDX measurement and trusted parameter set used by the on-chain TDX
  verifier.

Untrusted components:

- The remote request body.
- The remote caller.
- Host-side orchestration outside the measured image.
- External network input, including P2P peers and L1 RPC responses.
- Sync timing and availability.

Out of scope for this document:

- TDX/DCAP verifier deployment security.
- No-SSH, black-box image hardening.
- Sealed disk, dm-verity, snapshot rollback policy.
- Liveness, cost, monitoring, and alerting.
- Protocol bugs in measured taiko-client, measured node, gaiko2, or L1 contracts.

## Data Sources For Signed Commitment Fields

The audit must verify where every signed commitment field comes from.

| Commitment field | Source in TDX direct aggregate | Trust rule |
| --- | --- | --- |
| `firstProposalId` | Request proposal IDs after validation | Caller chooses target proposal range, but range must match local client state. |
| `firstProposalParentBlockHash` | Parent hash of the first local L2 header | Must come from local L2 node, not request metadata. |
| `lastProposalHash` | Local taiko-client proposal API for the last proposal | Must come from event-derived local metadata, not request metadata. |
| `actualProver` | Request | Economic attribution only; not enough to fake state transition. Needs policy review before permissionless use. |
| `endBlockNumber` | Last local L2 header in the last proposal range | Must match local event-covered range. |
| `endStateRoot` | Last local L2 header in the last proposal range | Must come from local L2 node. |
| transition `proposer` | Local taiko-client proposal API | Must come from event-derived local metadata. |
| transition `timestamp` | Local taiko-client proposal API | Must come from event-derived local metadata. |
| transition `blockHash` | Last local L2 header for each proposal | Must come from local L2 node. |
| aggregate signer instance | TDX signer identity | Must be the bootstrapped, registered TDX key. |

The request still contains proposal metadata fields for schema compatibility, but
the signed carry must not use request-supplied `proposal_hash`,
`parent_proposal_hash`, `transition.proposer`, or `transition.timestamp`.

## Required TDX-Side Checks

### 1. Proposal Metadata Must Be Local

Code points:

- `NewLocalProposalAPI`
- `LocalProposalAPI.ProposalMetadataByID`
- `TDXGethService.buildDirectAggregateCarry`

Required checks:

- The proposal API endpoint must be loopback-only.
- The proposal API response ID must equal the requested proposal ID.
- Proposal hash, parent proposal hash, proposer, and timestamp used in signed
  carry data must come from `ProposalMetadataByID`.
- Request-supplied proposal metadata must not overwrite local metadata before
  signing.

Why this matters:

- Blob/source commitments are included in `hashProposal(proposal)`.
- If TDX accepted host-supplied proposal hashes, the host could disconnect the
  signed commitment from the local event-derived proposal.
- Once the TDX service uses local `hashProposal`, a fake L1 view can only produce
  an L1-accepted proof if its final proposal hash equals the real L1
  `getProposalHash(lastProposalId)`.

### 2. Proposal IDs And Request Shape Must Be Contiguous

Code points:

- `ValidateDirectAggregateRequest`
- `validateDirectAggregateBlockNumbers`
- `validateDirectAggregateProposalContinuity`

Required checks:

- At least one proposal must be present.
- L2 block numbers inside each proposal must be non-empty and contiguous.
- Proposal IDs across the aggregate batch must be contiguous.
- Chain ID, verifier, and actual prover must be consistent across proposals.
- The first block of each later proposal must immediately follow the previous
  proposal's last block.

Why this matters:

- The direct aggregate path signs one aggregate commitment.
- The carry vector must describe one continuous proposal batch, not a caller
  assembled set of unrelated blocks.

### 3. Local Header Consistency Must Hold

Code points:

- `TDXGethService.buildDirectAggregateCarry`
- `requireHeaderProposalID`

Required checks:

- Every requested L2 header must be fetched from the local L2 node by number.
- Returned header number must equal the requested number.
- Every header in a proposal range must contain the expected Shasta proposal ID
  in `extraData`.
- Parent hash continuity must hold inside the proposal range.
- The left boundary block, if any, must belong to the previous proposal.
- The right boundary block must exist and belong to the next proposal.

Why this matters:

- This prevents a caller from omitting proposal blocks or extending the range
  into an adjacent proposal.
- The right boundary is intentionally strict. If the local node has not derived
  the next proposal block yet, the service should fail rather than guess that the
  current range is complete.

### 4. Event-Covered Range Must Be Certain

Code points:

- `TDXGethService.verifyDirectAggregateEventCoverage`
- local engine RPC `taikoAuth_lastCertainBlockIDByBatchID`
- local engine RPC `taikoAuth_lastCertainL1OriginByBatchID`

Required checks:

- Current proposal's certain last block must equal the last requested block.
- Previous proposal's certain last block must define the first requested block,
  with the proposal 1 genesis boundary handled explicitly.
- Last certain L1 origin block ID must equal the last requested L2 block number.
- Last certain L1 origin L2 block hash must equal the last local L2 header hash.
- Last certain L1 origin must include a non-empty L1 block hash and valid L1
  block height.

Why this matters:

- The service only signs blocks that the local client/engine marks as covered by
  L1-derived proposal state.
- P2P data and preconf-like data should not be enough to produce a proof if the
  local certain mapping does not cover the range.

### 5. Carry Vector Must Match Shasta Commitment Rules

Code points:

- `validateShastaProofCarryDataVec`
- `buildShastaCommitmentFromProofCarryDataVec`
- `hashShastaAggregationCarries`

Required checks:

- Carry vector must pass the common Shasta aggregate carry validation.
- Proposal ID continuity must still hold after local metadata is inserted.
- Parent proposal hash continuity must hold across locally fetched metadata.
- Parent block hash of each proposal must match the previous proposal checkpoint
  block hash.
- Chain ID, verifier, and actual prover must remain consistent.

Why this matters:

- These checks make direct aggregate equivalent to the normal aggregate
  commitment shape.
- The TDX service signs the aggregate public input hash, not an ad-hoc direct
  aggregate hash.

### 6. On-Chain Verification Must Close The Loop

Relevant on-chain checks in `Inbox.prove`:

- `_validateCommitment` enforces proposal bounds against the current L1 state.
- Parent block hash must match the current finalized parent or prior transition.
- `lastProposalHash` must equal `getProposalHash(lastProposalId)`.
- The verifier receives `hashCommitment(commitment)` and the proof.

Why this matters:

- Even if the TDX service sees a spoofed L1 RPC view, the real L1 contract checks
  the proof against the real L1 proposal hash and finalized parent state.
- A proof over a fake proposal hash should fail with `LastProposalHashMismatch`.
- A proof over a fake parent/checkpoint chain should fail parent continuity or
  verifier commitment checks.

## Attack Analysis

### Host Sends Fake Proposal Metadata

Expected result:

- No accepted fake proof.

Reason:

- `proposal_hash`, `parent_proposal_hash`, `proposer`, and `timestamp` from the
  request are ignored when building signed direct aggregate carry data.
- The signed values come from the measured local taiko-client proposal API.
- Tests cover that wrong request metadata is replaced by local metadata.

### Host Sends An Incomplete Block Range

Expected result:

- No proof.

Reason:

- The range must match `lastCertainBlockIDByBatchID`.
- The first block must match the previous proposal boundary.
- The next block must exist and belong to the next proposal.

### Local Node Is Behind

Expected result:

- Usually no proof.

Reason:

- Missing right boundary, missing certain range, or missing L1 origin makes direct
  aggregate fail.
- A stale but otherwise valid old proof does not advance real L1 because the L1
  finalized proposal ID and parent hash have already moved.

### P2P Fork Or Preconf Data Enters The Node

Expected result:

- No accepted fake proof under the audited profile.

Reason:

- Direct aggregate requires event-covered `lastCertain*` mappings and proposal ID
  boundaries.
- If P2P data is not covered by the local L1-derived certain mapping, the service
  should fail.
- The production TDX profile should still disable preconf proof generation unless
  there is a separate preconf safety design.

### Fake L1 RPC Feeds Fake Proposal Events

Expected result:

- No accepted fake proof unless the fake view yields the same real proposal hash
  and parent chain as real L1.

Reason:

- The real L1 `Inbox.prove` checks `lastProposalHash` against real
  `getProposalHash(lastProposalId)`.
- The real L1 also checks parent block hash continuity against finalized state.
- A fake L1 that produces different proposal metadata produces a proof that real
  L1 rejects.
- If the fake view produces the same proposal hash chain, it is equivalent for
  this commitment check except for freshness and liveness.

Residual concern:

- L1 RPC trust still matters for liveness, freshness, and operator confidence.
- Production should pin chain ID and contract addresses and may add L1 block
  finality/checkpoint policy.

### Local DB Is Rolled Back Or Corrupted

Expected result under the current threat model:

- No accepted fake transition if the measured node detects bad state or derives
  deterministically from the same parent and proposal sources.

Reason:

- The local node runs inside the measured TDX image and is part of the trusted
  computing base.
- Given the same parent state, proposal sources, and execution rules, the L2
  transition is deterministic.
- Missing or corrupted state should make the measured node fail to serve the
  required headers or certain mappings.
- A stale valid view cannot advance real L1 if the finalized parent/proposal state
  has already moved.

Residual concern:

- TDX does not automatically make host-backed disk immutable or rollback-safe.
- Deployment hardening should define sealed storage, rebuild-from-genesis, trusted
  checkpoint, or dm-verity policy before production.
- This is a deployment integrity topic, not a known direct aggregate commitment
  forgery path under the clean measured-runtime assumption.

### Caller Sets A Different `actual_prover`

Expected result:

- State transition safety is unchanged, but economic attribution may change.

Reason:

- `actualProver` is part of the signed commitment and is checked by the verifier
  commitment hash.
- It does not determine proposal hash, parent block hash, final block hash, or
  state root.
- L1 liveness bond handling can use `actualProver`, so permissionless deployment
  should decide whether this field may remain request-controlled.

## Audit Evidence To Keep Updated

Required local checks for this PR:

```bash
go test ./internal/prover
go test ./...
git diff --check
```

Important unit tests:

- `TestTDXGethServiceDirectAggregateUsesLocalProposalMetadata`
- `TestTDXGethServiceDirectAggregateRejectsBlockOutsideProposal`
- `TestTDXGethServiceDirectAggregateRejectsLeftBoundaryInsideProposal`
- `TestTDXGethServiceDirectAggregateRequiresRightBoundary`
- `TestTDXGethServiceDirectAggregateRejectsWrongRightBoundaryProposal`
- `TestTDXGethServiceDirectAggregateRequiresCertainLastBlockMapping`
- `TestTDXGethServiceDirectAggregateRejectsRangeOutsideCertainProposal`
- `TestTDXGethServiceDirectAggregateRejectsL1OriginBlockHashMismatch`
- `TestLocalProposalAPIProposalMetadataByID`
- `TestNewLocalProposalAPIRejectsExternalEndpoint`

Recommended devnet checks:

- Start measured-profile services with preconf proof generation disabled.
- Confirm local taiko-client proposal API is bound to loopback.
- Query a real proposal through local taiko-client API.
- Send a direct aggregate request with deliberately wrong request proposal
  metadata and confirm the proof carry uses local metadata.
- Send an incomplete block range and confirm the service fails.
- Send a request before the next proposal boundary exists and confirm the service
  fails.
- Submit a valid proof to the deployed devnet verifier or run the equivalent
  on-chain verifier call.

## Current Safety Conclusion

With the local proposal API and event-covered range checks enabled, the current
direct aggregate path has no known mechanism for a malicious caller to produce a
fake proof that is accepted by real L1, assuming the measured TDX image, measured
client, measured node, gaiko2 code, and L1 contracts are correct.

The remaining items are deployment and policy hardening:

- Build a no-SSH, black-box TDX image profile.
- Pin statement-affecting binaries, config, chain ID, Inbox address, verifier
  address, and trusted TDX parameters in the measured image or release manifest.
- Decide whether `actualProver` may remain request-controlled.
- Define storage rollback policy.
- Define L1 RPC freshness/finality policy.
- Add monitoring for sync lag, fork indicators, proposal boundary failures, and
  repeated invalid proof attempts.
