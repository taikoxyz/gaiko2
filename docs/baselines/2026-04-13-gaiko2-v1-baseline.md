# Gaiko2 V1 Baseline

This document captures the current `gaiko2` baseline after the first round of
design and implementation work. It is intended to be the reference point for
future Codex review, refinement, and follow-up changes.

## Goal

`gaiko2` exists to cross-check `raiko2` execution on `taiko-geth`, in the same
spirit that `gaiko` cross-checked `raiko`.

The baseline goal for `v1` is:

- accept a stable execution packet from `raiko2`,
- replay the packet with native `taiko-geth` stateless execution,
- confirm the replayed `stateRoot` and `receiptRoot` match the block headers,
- produce a proof envelope that can later be verified on chain.

`v1` is intentionally not a full Go preflight engine.

## Design Decisions Locked In

### 1. `v1` reuses canonical `raiko2` witness data

`gaiko2 v1` consumes the canonical witness and execution context produced by
`raiko2`, instead of independently rebuilding the whole preflight stack from
Go.

The reasoning is:

- this still provides meaningful `reth` vs `geth` execution cross-checking,
- it keeps the first version narrow and shippable,
- it does not prevent a later `v2` from adding independent preflight.

The consequence is also explicit:

- `v1` validates execution semantics,
- `v1` does not independently validate `raiko2`'s preflight / witness
  derivation logic.

### 2. The cross-check target is execution, not block header derivation

The main signal we care about is whether the same execution inputs produce the
same post-state and receipts on `taiko-geth`.

The replay baseline is therefore:

- match `stateRoot`,
- match `receiptRoot`,
- use the packet's ancestor header window to satisfy `BLOCKHASH`.

We do not treat "independently derive a new block hash sequence" as the core
`v1` success criterion.

### 3. `BLOCKHASH` requires more than the parent header

During implementation we confirmed that only carrying the parent header is not
enough. `anchorV4` can access up to the previous 255 block hashes, so the
execution packet must carry the relevant ancestor header window.

This is now part of the effective wire contract, even if the JSON schema keeps
the shape lightweight.

### 4. Adapter stays in `raiko2`

`gaiko2` should stay lightweight. The conversion from `raiko2` internal data
to the stable execution packet belongs in `raiko2`.

That gives us:

- one canonical adapter boundary,
- smaller `gaiko2` code surface,
- less churn when `raiko2` internals evolve.

### 5. Keep the protocol simple and versioned

The current wire contract is JSON with a top-level schema field:

```json
{
  "schema": "v1",
  "payload": { "...": "..." }
}
```

This is intentionally simple. Stability comes from the `schema` string, not
from the `raiko2` binary version.

### 6. One proof envelope, two signer modes

`gaiko2` now uses one proof envelope for both proving modes:

- `native`: sign with the fixed GoldenTouch private key, no quote attached.
- `tee`: sign with an enclave-managed private key and attach a TEE quote.

The replay pipeline and public-input hashing stay shared. Only the signer
source changes.

## Current Implementation Baseline

The following is in place inside `gaiko2`:

- stable request/response protocol in `internal/protocol`,
- request validation in `internal/prover/validate.go`,
- replay decoding from `raiko2`-adapted JSON into `taiko-geth` types,
- native stateless replay in `internal/prover/replay.go`,
- proof hashing in `internal/prover/hash.go`,
- signer abstraction in `internal/prover/signer.go`,
- `native` and `tee` proving modes,
- `ego` provider in `internal/tee`,
- internal HTTP API in `internal/api/server.go`,
- CLI entrypoint in `cmd/gaiko2/main.go`.

The current fork-specific prove endpoint is:

- `POST /prove/shasta`
- request body version is carried by `schema`, currently `v1`

The repository also contains a shared replay fixture:

- `testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`

That fixture is generated from the `raiko2` adapter and used to prove that the
current packet shape can really replay on `taiko-geth`.

## Verification Baseline

The current verification command is:

```bash
go test ./...
```

The baseline test coverage currently proves:

- protocol round-trips,
- request validation rules,
- adapter-compatible block / witness decoding,
- replay against the checked-in shared fixture,
- native proof signing,
- TEE signer behavior with a fake provider,
- HTTP success and error envelopes.

## Known Limits

These are known and accepted at this baseline:

- the real `ego` path compiles but has not yet been verified inside a live
  enclave in this repo,
- on-chain verifier contracts are not implemented here,
- `gaiko2` still assumes `raiko2` owns canonical preflight and packet
  construction,
- schema evolution policy is documented, but only request schema `v1` exists so
  far.

## Immediate Next Steps

When work resumes, the next useful steps are:

1. Finish the `raiko2 -> gaiko2` remote proving integration against this
   baseline.
2. Add a small end-to-end smoke path that exercises the HTTP server with the
   checked-in fixture.
3. Validate the `tee` mode inside an actual `ego` runtime.
4. Define the first on-chain verifier surface for `native` and `tee` outputs.
