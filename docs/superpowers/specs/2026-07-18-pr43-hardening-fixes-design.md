# PR 43 Hardening Follow-up Design

## Context

PR #43 hardens TEE bootstrap, HTTP request handling, configuration validation, and server shutdown. Review found that four guarantees were incomplete:

- the non-force bootstrap guard was a check-then-replace race;
- `MaxBytesReader` was not observed when a decoder stopped after one valid JSON value;
- file renames were not made durable by syncing their parent directories, and a failed bootstrap metadata write could strand an already-saved key;
- direct `bootstrap --force` use emitted no destructive-action warning.

A smaller shutdown race could also hide an unexpected `Serve` failure when cancellation was selected at the same time.

## Goals

- Make the final filesystem operation enforce non-force no-replace semantics.
- Preserve the sealed key if bootstrap metadata persistence is interrupted, and make retry recover metadata without rotating that key.
- Guarantee that a request whose body exceeds the configured limit cannot reach validation or proving.
- Make renamed key and JSON files durable on the target Linux filesystem.
- Warn before any forced key replacement.
- Preserve unexpected server errors during graceful shutdown.
- Cover every reviewed failure mode with a regression test that fails before its implementation.

## Non-goals

- Changing the 512 MB production limit.
- Adding body or proof execution timeouts.
- Changing proof schemas or validation rules.
- Building a general multi-file transaction framework.
- Supporting non-POSIX key persistence targets; the EGo deployment target is Linux/SGX.

## Considered Approaches

### 1. Atomic no-replace plus metadata recovery — selected

The provider receives the overwrite intent when saving a key. Non-force save installs a fully synced same-directory temporary file with an atomic no-replace operation; force save atomically renames over the destination. If the key exists but its bootstrap JSON is absent, corrupt, or describes another address, the CLI reconstructs metadata from the existing sealed key rather than generating another key.

This closes the race at the operation that matters, retains the current provider boundary, and gives interrupted bootstrap runs a recovery path without a journal.

### 2. Advisory lock plus transaction journal

Hold a filesystem lock across key generation, quote creation, key installation, and JSON persistence, while recording a pending transaction for crash recovery. This provides explicit serialization but adds lock lifecycle, stale journal, and recovery-state complexity that is unnecessary when no-replace installation and deterministic recovery are sufficient.

### 3. Lock only the existing check/save sequence

This is a smaller change, but all callers would have to honor the lock and a crash after key persistence would still strand metadata. It does not satisfy the recovery goal.

## Detailed Design

### Key installation and recovery

`Provider.SavePrivateKey` will accept an overwrite flag. `Bootstrap` may retain the early existence check to avoid expensive key and quote generation, but correctness will not depend on it. The final non-force save must fail with `ErrPrivateKeyExists` if another process installed a key after the early check.

The atomic file helper will:

1. create a temporary file in the destination directory;
2. write all data, apply the requested mode, sync, and close it;
3. install it with no-replace semantics for non-force writes or rename replacement for force writes;
4. remove the temporary name where needed;
5. open and sync the parent directory before returning success.

On Linux, linking the fully written same-directory temporary file to an absent destination provides atomic no-replace installation. An existing destination maps to `ErrPrivateKeyExists`. Force writes continue to use rename replacement. JSON writes use rename replacement followed by parent-directory sync.

When the CLI receives `ErrPrivateKeyExists`, it compares the sealed key identity with `bootstrap.gaiko2.json`:

- matching metadata means the release is already bootstrapped, so the existing refusal and `--force` guidance are returned;
- missing, syntactically invalid, or identity-mismatched metadata is reconstructed from the existing private key and a fresh quote, then atomically saved and printed;
- the recovery path never calls `SavePrivateKey` and therefore never rotates the identity.

Filesystem access errors other than a missing metadata file are returned rather than treated as recoverable corruption.

This also repairs the state left by a crash or write error after key installation but before metadata persistence.

### Exact request-body enforcement

Both prove handlers will use one shared decode helper. It will:

1. reject a known `Content-Length` greater than the limit before decoding;
2. wrap the body with `http.MaxBytesReader`;
3. decode the expected request value;
4. perform a second decode and require `io.EOF`, thereby consuming trailing whitespace and surfacing a delayed `MaxBytesError`;
5. map every `MaxBytesError` to `413 REQUEST_TOO_LARGE` and other decode failures, including a second JSON value, to `400 INVALID_JSON`.

Only a successfully bounded, single JSON document reaches validation or the prover service.

### Force warning

The bootstrap command will emit a warning to an injectable stderr writer before invoking a force bootstrap. The message will state that an existing key and any on-chain registration bound to it become unusable. The warning is emitted even when no key currently exists because `--force` explicitly authorizes replacement and the existence check is inherently racy.

### Shutdown error preservation

After a successful `Shutdown`, `serveHTTP` will consume the `Serve` result. `http.ErrServerClosed` remains a clean exit; any other error is returned. A small helper will make this result classification deterministic to test.

## Error Handling

- Atomic no-replace collisions return the existing `ErrPrivateKeyExists` sentinel.
- Metadata recovery errors identify whether loading the sealed key, producing its quote, or saving repaired JSON failed.
- Temporary files are removed on ordinary failure; a process crash may leave an inert temporary file, but never a partial destination file.
- Parent-directory sync failures are returned to the caller rather than reporting a durable write.
- Body-limit errors retain the standard proof error envelope.

## Test Strategy

Tests will be added and observed failing before production changes:

- concurrent non-force saves cannot both install a destination key;
- force save replaces an existing key while non-force save preserves it;
- bootstrap recovery reconstructs matching metadata without saving a new key;
- directory-sync and atomic-install error paths clean up temporary files where applicable;
- known-length and chunked valid-prefix bodies exceeding the limit return 413 for both prove routes;
- trailing whitespace within the limit is accepted, a second JSON value is rejected as invalid JSON, and ordinary malformed JSON remains 400;
- direct `--force` emits the destructive warning;
- shutdown returns a queued unexpected serve error and treats `http.ErrServerClosed` as clean.

After focused red/green cycles, verification will run `go test ./...`, `go test -race` for the changed packages, `go vet ./...`, `gofmt -l .`, and `git diff --check`.

## Rollout and Compatibility

The changes are internal to the CLI, HTTP handlers, and TEE provider interface. Existing command syntax and response envelopes remain compatible. The only new successful path is recovery of inconsistent bootstrap metadata using the already-sealed identity; a consistently bootstrapped release continues to refuse non-force bootstrap.
