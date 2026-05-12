# gaiko2 Remote Prover Integration Design

**Goal:** Align `gaiko2` with the `raiko2` remote prover protocol by keeping the current HTTP routes and payload shape, while renaming the protocol surface so `raiko2` owns the request and response schemas.

## Context

`gaiko2` currently exposes a Shasta replay service over:

- `POST /prove/shasta`
- `POST /prove/shasta-aggregate`
- `GET /healthz`

The runtime behavior behind those routes is already correct for the current remote prover use case. The integration change is architectural rather than semantic:

- `raiko2` now owns the remote prover protocol contract,
- `gaiko2` becomes a provider implementation of that contract,
- the packet shape stays the same, but schema names move from `gaiko2`-owned names to `raiko2`-owned names.

This change must not alter replay semantics, TEE signing behavior, SGX deployment flow, or route names.

## Scope

This integration changes only the protocol surface:

- proposal request schema name,
- aggregate request schema name,
- proof response schema name,
- request validation code,
- checked-in fixtures and tests that assert old schema names,
- documentation that still describes the old names.

This integration does not change:

- replay execution,
- witness decoding,
- proof generation,
- aggregate hashing,
- verifier registration,
- SGX attestation,
- release deployment layout.

## Inputs

The design is based on the contract captured in:

- [gaiko2-remote-prover-integration.md](../../gaiko2-remote-prover-integration.md)

## Decision

Perform a direct cutover to the `raiko2` protocol names with no compatibility layer.

Accepted request schemas:

- proposal request: `raiko2-shasta-request-v1`
- aggregate request: `raiko2-shasta-aggregate-request-v1`

Returned response schema:

- `raiko2-proof-v1`

Rejected approach:

- accepting both old and new schema names,
- returning mixed old/new response schemas depending on caller,
- adding new HTTP routes.

The caller is expected to be updated in lockstep. `gaiko2` is the first provider implementation of the `raiko2` remote prover contract, so carrying transitional compatibility in `gaiko2` is unnecessary complexity.

## Required Internal Change

The current code shares one request schema constant between proposal requests and aggregate requests. That is no longer sufficient.

`gaiko2` must split protocol constants into three independent names:

- proposal request schema constant,
- aggregate request schema constant,
- proof response schema constant.

This is the only meaningful design correction needed beyond the source document. Without this split, the implementation would either:

- validate aggregate requests against the wrong name, or
- reintroduce special cases outside the protocol package.

The protocol package should remain the single source of truth for all three strings.

## Data Flow

The runtime request flow remains:

1. `raiko2` sends a JSON proposal packet to `POST /prove/shasta`.
2. `gaiko2` decodes it into `protocol.ShastaRequest`.
3. `gaiko2` validates:
   - request schema name,
   - replay block continuity,
   - carry/checkpoint consistency.
4. `gaiko2` replays the witness and builds a proof result.
5. `gaiko2` returns a `protocol.ProofResponse` with schema `raiko2-proof-v1`.

The aggregate flow remains:

1. `raiko2` sends a JSON aggregate packet to `POST /prove/shasta-aggregate`.
2. `gaiko2` decodes it into `protocol.ShastaAggregateRequest`.
3. `gaiko2` validates:
   - aggregate request schema name,
   - proof input self-consistency,
   - aggregate carry sequence,
   - signer metadata consistency.
4. `gaiko2` aggregates and signs the result.
5. `gaiko2` returns a `protocol.ProofResponse` with schema `raiko2-proof-v1`.

## Files Affected

Implementation will touch these code paths:

- [internal/protocol/shasta_v1.go](/home/yue/works/taiko/gaiko2/internal/protocol/shasta_v1.go)
- [internal/prover/validate.go](/home/yue/works/taiko/gaiko2/internal/prover/validate.go)
- [internal/prover/aggregate_validate.go](/home/yue/works/taiko/gaiko2/internal/prover/aggregate_validate.go)

Tests and fixtures that must be updated:

- [internal/protocol/shasta_v1_test.go](/home/yue/works/taiko/gaiko2/internal/protocol/shasta_v1_test.go)
- [internal/prover/validate_test.go](/home/yue/works/taiko/gaiko2/internal/prover/validate_test.go)
- [internal/prover/aggregate_test.go](/home/yue/works/taiko/gaiko2/internal/prover/aggregate_test.go)
- [internal/api/server_test.go](/home/yue/works/taiko/gaiko2/internal/api/server_test.go)
- [internal/prover/replay_fixture_test.go](/home/yue/works/taiko/gaiko2/internal/prover/replay_fixture_test.go)
- [testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json](/home/yue/works/taiko/gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json)
- [testdata/README.md](/home/yue/works/taiko/gaiko2/testdata/README.md)

Documentation that should be synchronized:

- [README.md](/home/yue/works/taiko/gaiko2/README.md)
- [docs/deployment/masaya-fork-window-regression.md](/home/yue/works/taiko/gaiko2/docs/deployment/masaya-fork-window-regression.md)

## Testing Strategy

Local verification inside `gaiko2`:

- `go test ./internal/api ./internal/prover ./internal/protocol ./cmd/gaiko2`

Black-box conformance against `raiko2` must be run from a `raiko2` checkout, not from the `gaiko2`
repository root:

```bash
cd /home/yue/works/taiko/raiko2
RAIKO2_REMOTE_PROVER_BASE_URL=http://127.0.0.1:8080 \
cargo test -p raiko2-prover --no-default-features \
  --test remote_prover_conformance -- --ignored --nocapture
```

Acceptance criteria:

- proposal conformance test passes,
- aggregate conformance test passes,
- both endpoints still return `200` with `schema = "raiko2-proof-v1"` on success,
- validation failures still return the existing error envelope shape.

## Risks

### 1. Partial schema renaming

If proposal and aggregate requests keep sharing one schema constant, one of the two endpoints will validate the wrong name. The implementation must split the constants first.

### 2. Fixture drift

Several tests read checked-in JSON fixtures instead of constructing requests inline. If the shared fixture is not updated, the API and replay fixture tests will fail even if the protocol package is correct.

### 3. Doc drift

The top-level README, Masaya regression notes, and shared testdata documentation still mention the old schema names. Leaving those stale will create operator confusion during future regression runs.

## Non-Goals

This work does not:

- add a submodule,
- vendor `raiko2`,
- change SGX image release flow,
- change request field layout,
- change proof encoding,
- rename HTTP routes,
- add compatibility for the old `"v1"` request schema,
- preserve `gaiko2-proof-v1` in responses.
