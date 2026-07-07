# Shasta Canonical Fork Derivation Fix Design

## Context

The security report identifies one proof-soundness issue in Shasta manifest
validation. `ValidateGuestInputManifestBindingWithContext` currently reads
Shasta and Unzen fork metadata from `witness.chain_spec.hard_forks`, which is
part of caller-supplied guest input. That request-controlled metadata selects:

- the Shasta timestamp lower bound used to derive source manifest block
  timestamps
- the Unzen source manifest block-count limit, switching between 192 and 768
  blocks

If an attacker moves those fork timestamps in the request, gaiko2 can validate
an internally consistent but non-canonical derivation result before signing the
Shasta proof input.

## Scope

This fix is limited to the report finding: canonical Shasta and Unzen
derivation metadata. It does not change verifier address selection or other
uses of request `chain_spec` outside the derivation metadata path.

## Architecture

Manifest validation will use canonical chain configuration as the only source
for Shasta and Unzen derivation metadata. A small helper will resolve the
canonical config with `chainConfigFor(view.GuestInputChainID)` and return:

- `forkTimestamp`: `*config.ShastaTime`, required for Shasta derivation
- `maxBlocks`: `shastaUnzenDerivationSourceLimit` when
  `config.IsUnzen(proposal.Timestamp)` is true, otherwise
  `shastaDerivationSourceMaxBlocks`

Unsupported chain IDs will fail validation through `chainConfigFor`. A missing
canonical `ShastaTime` will fail with a clear validation error because Shasta
manifest derivation cannot safely infer its fork boundary from request data.

## Data Flow

The validation flow will remain:

`DecodeGuestInput` -> `ValidateGuestInputCarry` -> `ValidateGuestInputBlobSources`
-> `ValidateGuestInputManifestBindingWithContext` -> replay validation.

Inside manifest binding, the fork values will change from:

`view.Witnesses[0].ChainSpecRaw` -> witness hard-fork parser -> derivation
parameters

to:

`view.GuestInputChainID` -> `chainConfigFor` -> canonical derivation parameters.

`guest_input.witnesses[0].chain_spec.hard_forks` will no longer influence source
manifest timestamps or source manifest block-count limits.

## Error Handling

- Unsupported chain IDs return the existing `chainConfigFor` unsupported-chain
  error through manifest validation.
- Missing canonical `ShastaTime` returns an explicit validation error.
- Inactive or absent canonical Unzen keeps the pre-Unzen 192-block limit.
- Active canonical Unzen uses the 768-block limit.

## Tests

Add focused tests in `internal/prover/manifest_validate_test.go`:

- A request-provided future `SHASTA.Timestamp` must not make gaiko2 accept a
  derived block timestamp above the proposal timestamp. The canonical config
  determines the lower bound, so the crafted request should fail validation.
- A request-provided active `UNZEN` fork must not raise the source manifest
  block limit when canonical Unzen is inactive. The manifest decode should use
  the canonical 192-block limit and reject oversized source manifests.

Existing manifest binding, replay, and full Go tests should continue to pass.

## Non-Goals

- Do not change `resolveGuestInputVerifier` or verifier address fork selection.
- Do not redesign guest input decoding.
- Do not add network lookups or external config files.
- Do not alter replay execution or proof signing behavior.
