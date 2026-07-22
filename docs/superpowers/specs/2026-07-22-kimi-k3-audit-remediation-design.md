# Kimi K3 Audit Remediation Design

**Date:** 2026-07-22
**Status:** Approved for implementation planning

## Goal

Remediate the valid findings in `docs/audits/kimi-k3-audit.md` without
disrupting the existing explicit native-mode development workflow. Correct the
audit where its exploit claims do not match the current code or protocol.

## Current-state assessment

The audit combines one replay fail-closed gap, one startup configuration gap,
an inherent warning about the public native mock signer, and several
defense-in-depth observations.

### Finding 1: deferred StateDB errors

This is a valid fail-closed gap. `GethRunner.Execute` does not inspect
`StateDB.Error()` after block processing or after `IntermediateRoot`. The pinned
taiko-geth `BlockValidator.ValidateState` returns before its root comparison in
stateless mode, so it does not surface the deferred error for gaiko2.

The report's transaction-read attack is broader than the reachable production
path. GuestInput manifest filtering already checks the same `StateDB.Error()`
after system calls and transaction execution. The independent replay stage must
still enforce the invariant because post-execution processing, finalization,
and intermediate-root calculation can introduce deferred errors, and because
soundness should not depend on an earlier duplicate execution remaining
perfectly equivalent.

### Findings 2 and 3: native mock signer

These describe an operational configuration hazard, not a separately fixable
aggregate-endpoint or key-reuse vulnerability. Native mode deliberately uses a
published deterministic key for local and development regression output. If
that signer is registered in a verifier protecting real value, anyone can sign
the final aggregation digest directly; disabling the HTTP aggregation endpoint
or replacing the key with another published deterministic key would not
prevent forgery.

The remediation is to make the existing trust boundary unmistakable. Explicit
native mode remains available, but startup emits a prominent warning and the
documentation states that the mock signer must never be registered in a
value-bearing verifier.

### Finding 4: empty proving mode

This is valid. An unset `GAIKO2_PROVING_MODE` currently selects native mode.
Startup must instead fail before opening a listener. Explicit
`GAIKO2_PROVING_MODE=native` remains sufficient; no additional development flag
is introduced.

### Findings 5, 6, 8, and 9

These remain informational hardening notes, not current soundness bugs:

- The requester-provided verifier is included in the signed digest and an
  unauthorized verifier is rejected on-chain. Replay uses the hard-coded chain
  configuration selected by the carry chain ID rather than the auxiliary
  witness chain-spec fields.
- The current Unzen zk-gas schedule and recoverable transaction classifier match
  the pinned taiko-geth dependency and the inspected alethia-reth implementation.
  Future dependency changes still require parity review.
- All reachable `u48Word` inputs are range-checked before hashing, including the
  aggregate path.
- `witness.accounts` is part of the raiko2 wire input, but gaiko2 derives and
  executes state from the authenticated witness trie. Treating the duplicate
  account map as an additional authority would not improve soundness.

No runtime changes are included for these observations.

### Finding 7: Anchored event third word

This finding is refuted. `Anchored(uint48,uint48,bytes32)` emits
`ancestorsHash` as its third word, not the `anchorV4` checkpoint block hash.
Cross-checking it against the calldata block hash would reject correct blocks.
The audit must be corrected rather than the implementation changed.

## Runtime design

### Replay state error checks

`GethRunner.Execute` will enforce the deferred-state-error invariant at two
phase boundaries:

1. Immediately after `processReplayBlock` succeeds and before subsequent state
   validation.
2. Immediately after `IntermediateRoot` computes the candidate state root.

Both checks apply to legacy and Unzen execution because they live in the shared
`GethRunner.Execute` path. Errors will include the phase and wrap the underlying
`StateDB.Error()` value. No duplicate check is needed inside
`processUnzenReplayBlock`.

The first check rejects deferred errors from transaction execution, system
calls, post-execution queues, or finalization. The second rejects errors first
encountered while opening or updating tries during root calculation.

### Proving-mode startup behavior

`NewConfiguredReplayService` will reject an empty normalized `ServiceConfig.Mode`
with an error that identifies `GAIKO2_PROVING_MODE` and the accepted values.
Keeping the rule in the service constructor gives CLI and programmatic callers
the same behavior.

The server command will construct the configured service before printing a
successful startup summary or opening the TCP listener. This prevents an empty
or invalid mode from producing a misleading startup message.

When the normalized mode is `native`, the command will print a warning that:

- the signing key is public and deterministic;
- native mode is only for local or development use; and
- the native signer must never be registered in a verifier protecting real
  value.

Explicit native commands, the native Docker image, and the native Compose
service continue to work because all already set `GAIKO2_PROVING_MODE=native`.

## Audit documentation changes

`docs/audits/kimi-k3-audit.md` will preserve the original review context while
updating its summary table and affected finding text to reflect the validated
status:

- Finding 1 remains valid, with the reachable gap and remediation described
  accurately.
- Findings 2 and 3 are reclassified as native-mode operational warnings.
- Finding 4 is marked valid and remediated.
- Findings 5, 6, 8, and 9 remain informational hardening notes.
- Finding 7 is marked refuted and corrected to identify `ancestorsHash`.

The document will not claim that disabling native aggregation or publishing a
different mock key prevents forgery.

## Error handling

- Deferred state errors fail the proof before any signature is produced.
- Empty or unsupported proving modes fail server startup before listening.
- Explicit native mode remains functional and produces a clear warning rather
  than a runtime request error.
- Existing HTTP routes, schemas, proof envelopes, signer output, and TEE
  behavior do not change.

## Test design

Focused tests will cover:

1. A `StateDB` with a deferred missing-witness error is rejected by the shared
   replay-state guard.
2. The guard is called after block processing and after intermediate-root
   computation, with phase-specific error context.
3. `NewConfiguredReplayService` rejects an empty mode.
4. Explicit native mode still constructs a service successfully.
5. The server fails before invoking `listen` when the mode is empty.
6. Explicit native startup prints the public-key development warning.
7. Existing native proof and aggregate regressions remain unchanged.

The implementation will be verified with:

```bash
go test ./... -count=1
go vet ./...
git diff --check
```

## Non-goals

- Adding authentication to prover endpoints.
- Introducing `GAIKO2_DEV_MODE` or another native-mode opt-in.
- Disabling native aggregation.
- Rotating the deterministic native signing key.
- Changing on-chain verifier registration or deployment configuration.
- Adding new chain-spec, zk-gas differential, `uint48`, or account-map runtime
  validation as part of this remediation.

## Success criteria

- No proof can be signed after replay records a deferred state database error.
- An unset proving mode cannot start a server.
- `GAIKO2_PROVING_MODE=native` retains its current behavior and emits an
  unmistakable safety warning.
- The audit no longer presents refuted or configuration-only observations as
  confirmed code-level soundness vulnerabilities.
- The full Go test suite, vet, and diff checks pass.
