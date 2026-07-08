# Pre-Unzen Difficulty Validation Design

## Goal

Fix the Shasta manifest soundness gap where gaiko2 can sign a proof-carry checkpoint whose block hash includes a non-canonical pre-Unzen block difficulty.

## Context

The security report identified that `decodeReplayBlock` reads attacker-controlled replay block JSON `difficulty` into `types.Header.Difficulty`. That value participates in the Ethereum block hash used by `proof_carry_data.transition_input.checkpoint.blockHash`.

`validateManifestBlockBinding` already checks neighboring hash-relevant fields such as timestamp, coinbase, gas limit, extra data, mix hash, and transaction root. It does not check the current block difficulty. Canonical Taiko derivation and consensus reject nonzero difficulty before Unzen, while gaiko2 currently preserves pre-Unzen difficulty during replay.

## Design

Add a focused header validation step in the Shasta manifest block binding path.

For the current replay block:

- Resolve the canonical chain config with `chainConfigFor(view.GuestInputChainID)`.
- Use `config.IsUnzen(header.Time)` to decide whether Unzen rules are active.
- Before Unzen, require `header.Difficulty` to be present and equal to zero.
- During Unzen, leave the existing replay path responsible for preserving imported zk-gas difficulty separately and zeroing execution difficulty before replay.

This keeps the invariant at the manifest binding boundary: gaiko2 must reject hash-relevant header fields that canonical Taiko clients would reject before the prover signs the proof carry data.

## Error Handling

Return a clear validation error when the pre-Unzen difficulty is missing or nonzero. The error should include `difficulty` so callers and tests can identify the rejected invariant.

Do not silently normalize pre-Unzen difficulty to zero. Normalization would compute a different block hash than the request supplied and could hide malicious input.

## Testing

Add a focused regression test in `internal/prover/manifest_validate_test.go`.

The test should:

- Build the existing valid manifest binding fixture.
- Switch the fixture to Taiko mainnet chain ID and Shasta-era timestamps so canonical Unzen rules are inactive.
- Keep the synthetic ancestry, L2 contract address, anchor recipient, base fee, and test transaction coherent after the chain switch.
- Mutate the replay block difficulty to `1`.
- Let the fixture recompute the proof-carry checkpoint block hash from the mutated block.
- Call `ValidateGuestInputManifestBinding`.
- Assert that validation rejects the input with an error containing `difficulty`.

Run the targeted prover test before implementation to verify the regression fails, then run it again after the validator change. Run the relevant prover package tests afterward.

## Scope

This fix is intentionally limited to the reported finding. It does not refactor the manifest validator or add broader hash-relevant header validation beyond the current block difficulty rule.
