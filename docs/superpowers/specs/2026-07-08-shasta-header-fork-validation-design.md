# Shasta Header Fork Validation Design

## Context

The security scan at `/private/var/folders/1w/lmxbz76d2m15b1_l9qkjbstw0000gn/T/codex-security-scans/gaiko2/7736988_20260708T060904Z/report.md` reports one high-severity proof-soundness issue. The `/prove/shasta` path accepts replay block headers with optional hash-affecting fields, validates execution and manifest-derived data, then signs proof carry data that includes the checkpoint block hash.

The missing invariant is that gaiko2 must reject block headers whose fork-specific fields would be rejected by canonical Taiko derivation or taiko-geth consensus before the proof result is signed.

## Scope

This change is narrowly scoped to the reported finding:

- Validate Shasta replay block header fork fields for `BlobGasUsed`, `ExcessBlobGas`, `ParentBeaconRoot`, `RequestsHash`, and `SlotNumber`.
- Add regression tests for pre-Unzen, Unzen, and Amsterdam-related behavior using the existing manifest validation fixture.
- Do not refactor request decoding, replay execution, proof hashing, signer behavior, API ingress, or verifier configuration.

## Selected Approach

Add a small fork-aware validator in `internal/prover/manifest_validate.go`, near the existing header base-fee and difficulty validators, and call it from `validateManifestBlockBinding`.

This follows the current validation shape:

- `decodeHeader` remains a format decoder and does not need chain or fork context.
- Manifest validation already has the chain ID, block header timestamp, and canonical Taiko chain config.
- Rejection happens during `ValidateRequestWithContext`, before replay execution and proof signing.

Directly calling taiko-geth consensus verification is not selected for this PR because it would require more execution context and create a larger behavioral surface than this fix needs.

## Validation Rules

The validator resolves the canonical chain config with `chainConfigFor(chainID)` and applies fork checks from the block header timestamp.

Pre-Unzen blocks:

- `BlobGasUsed` must be absent.
- `ExcessBlobGas` must be absent.
- `ParentBeaconRoot` must be absent.
- `RequestsHash` must be absent, including an explicit empty requests hash.

Unzen and later blocks:

- `BlobGasUsed` must be present and equal to `0`.
- `ExcessBlobGas` must be present and equal to `0`.
- `ParentBeaconRoot` must be present and equal to the zero hash.
- `RequestsHash` must be present and equal to `types.EmptyRequestsHash`.

Slot number:

- Determine Amsterdam activation with `config.IsAmsterdam(header.Number, header.Time)`.
- Pre-Amsterdam headers must not contain `SlotNumber`.
- Amsterdam and later headers must contain `SlotNumber`.
- No additional slot-number value derivation is introduced in this PR because the local taiko-geth header-level rule validates presence, not a derived numeric value.

## Error Handling

Each rejection should return a plain validation error that names the invalid field and the fork context, for example:

- `pre-Unzen blob_gas_used must be absent`
- `Unzen requests_hash mismatch: expected <empty requests hash> got <actual>`

The errors should remain deterministic and suitable for table-test substring assertions.

## Tests

Add focused tests in `internal/prover/manifest_validate_test.go` using the existing manifest binding fixture. The tests should call `ValidateGuestInputManifestBinding` so they cover the actual request-validation path.

Coverage:

- Pre-Unzen rejects present `blobGasUsed`, `excessBlobGas`, `parentBeaconBlockRoot`, and `requestsHash`.
- Unzen rejects missing values for `blobGasUsed`, `excessBlobGas`, `parentBeaconBlockRoot`, and `requestsHash`.
- Unzen rejects non-zero or non-empty values for those same fields.
- Slot-number cases cover the local taiko-geth Amsterdam activation behavior.

Verification commands:

- `go test ./internal/prover`
- `go test ./...`

## PR Plan

After the spec is approved:

1. Create a detailed implementation plan with the required planning workflow.
2. Implement the validator and call it from manifest block binding.
3. Add the regression tests.
4. Run the verification commands.
5. Commit the code changes, push the branch, and open a PR.
