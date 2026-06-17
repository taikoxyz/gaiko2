# TDX Node-Based POC Track

Date: 2026-06-17

## Status

Historical POC track for PR #6.

This document preserves the node-based `tdxgeth` direction that PR #6 explored.
It is not the preferred next production direction after the June 17 discussion.
The active direction is expected to reuse the SGX-style remote prover flow:
raiko2 builds canonical `GuestInput`, the TDX service replays that input, and
the TDX-specific part is bootstrap, attestation, signing identity, verifier
registration, and proof-type routing.

Keep this track so the POC remains understandable and recoverable.

## POC Statement

The node-based `tdxgeth` POC statement is:

```text
A registered TDX instance, running an accepted measured VM image, checked the
same Shasta proposal range against taiko-geth and taiko-client inside that VM
and signed the canonical Shasta commitment hash.
```

This is different from the SGX/Gaiko2 guest-input replay statement:

```text
A registered TEE instance replayed the host-provided canonical raiko2 GuestInput
and signed the canonical Shasta input hash.
```

The POC does not prove that TDX executed the raiko2 guest. It proves a measured
local-node acceptance statement, assuming the measured VM image, local node,
local client, attestation path, and signer identity are accepted.

## Code And Docs In This Track

The POC lives in this PR branch and includes:

- `internal/prover/tdxgeth.go`
- `internal/prover/local_l2.go`
- `internal/prover/local_proposal.go`
- `internal/tee/tdx.go`
- `tdx/`
- `docs/deployment/tdx-gaiko2.md`
- `docs/plans/2026-06-15-tdx-direct-aggregate-security-notes.md`
- `docs/plans/2026-06-15-tdx-direct-aggregate-safety-audit.md`

Those files should be read as one experimental track. Do not mix their security
statement into the newer TDX guest-input replay direction without rewriting the
boundary.

## Why This Was Deprioritized

The node-based track is heavier and harder to audit than SGX-style replay:

- It makes the measured VM image include more statement-affecting components:
  gaiko2, taiko-geth, taiko-client, tdxs, service units, config, and storage
  policy.
- It introduces local sync state, L1 RPC freshness, storage rollback, and local
  node/client correctness into the proof boundary.
- It needs TDX-local replacement checks for invariants that raiko2 already
  validates in the canonical `GuestInput` path.
- It is harder to explain as a prover backend because it signs a local-node
  acceptance statement, not a canonical raiko2 guest execution statement.

The POC is still useful as an independent measured-node attestation experiment
and as prior art for future TDX verifier, image identity, and signer lifecycle
work.

## Minimum Safety Bar For This POC

If this track is resumed, do not treat it as production-ready unless all of the
following are true:

- The measured image pins gaiko2, taiko-geth, taiko-client, tdxs, systemd units,
  startup scripts, and statement-affecting config.
- Bootstrap creates or unseals the TDX signer inside the measured VM.
- Registration binds the signer address to the accepted measured image identity.
- The local taiko-geth RPC is loopback-only or VM-local.
- The local taiko-client proposal API is loopback-only or VM-local.
- Direct aggregate signing uses local proposal metadata and local L2 headers,
  not host-supplied proposal hashes or timestamps.
- The service checks event-covered proposal ranges using local engine/client
  state before signing.
- L1 RPC trust/freshness and storage rollback policy are explicitly defined.

## Relationship To The New Direction

The preferred new direction should not depend on local taiko-geth inside TDX.
It should:

- keep the remote prover HTTP API shape compatible with SGX;
- let raiko2 build and validate canonical `GuestInput`;
- add a distinct TDX proof type and lane on the host side;
- keep TDX and SGX verifier addresses, instance IDs, and registration paths
  separate;
- implement TDX-specific bootstrap and quote/signing provider logic inside the
  provider runtime.

That newer direction can reuse much of the SGX remote prover protocol, but it
must not reuse SGX identity or verifier registration semantics silently.
