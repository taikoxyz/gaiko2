# Kimi K3 Audit Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make replay fail closed on deferred state database errors, require an explicit proving mode while preserving explicit native mode, and correct the Kimi K3 audit classifications.

**Architecture:** Add one shared replay-state guard around the existing `GethRunner.Execute` phase boundaries, enforce proving-mode selection in the configured service constructor, and order CLI startup so validation precedes success output and listening. Keep native signing, aggregation, proof formats, and deployment manifests unchanged; document the public native signer as a development-only trust boundary.

**Tech Stack:** Go 1.24, taiko-geth `StateDB`, standard-library CLI/HTTP code, Go `testing`, Markdown documentation.

## Global Constraints

- Preserve `GAIKO2_PROVING_MODE=native` as the only opt-in needed for the current native development workflow.
- Do not add `GAIKO2_DEV_MODE`, disable native aggregation, or rotate the deterministic native signing key.
- Do not change HTTP routes, request schemas, proof envelopes, signer output, TEE behavior, Compose, or Docker behavior.
- Do not add runtime changes for audit Findings 5, 6, 8, or 9.
- Correct Finding 7 as refuted: the third `Anchored(uint48,uint48,bytes32)` word is `ancestorsHash`, not an anchor block hash.
- Every deferred `StateDB.Error()` must abort replay before signing.
- Follow test-driven development for Go changes and commit each independently reviewable task.
- Approved design: `docs/superpowers/specs/2026-07-22-kimi-k3-audit-remediation-design.md`.

---

## File structure

- `internal/prover/replay.go`: own the shared deferred-state-error guard and invoke it at replay phase boundaries.
- `internal/prover/replay_test.go`: verify guard behavior and wrapped phase context.
- `internal/prover/signer.go`: reject empty configured proving mode while retaining explicit native and TEE selection.
- `internal/prover/signer_test.go`: lock down empty-mode rejection and explicit-native compatibility.
- `cmd/gaiko2/main.go`: validate the service before success output/listening and print the native-mode safety warning.
- `cmd/gaiko2/main_test.go`: verify startup ordering, listener suppression, warning output, and existing explicit-native server paths.
- `README.md`: document required mode selection and the native mock signer's trust boundary.
- `docs/audits/kimi-k3-audit.md`: replace incorrect exploit classifications and recommendations with the validated assessment.

---

### Task 1: Reject deferred state errors during replay

**Files:**
- Modify: `internal/prover/replay.go:43-88`
- Test: `internal/prover/replay_test.go:3-42`

**Interfaces:**
- Consumes: `Error() error` from `*state.StateDB`.
- Produces: `replayStateError(source replayStateErrorSource, phase string) error`, used only by `GethRunner.Execute`.

- [ ] **Step 1: Write failing replay-state guard tests**

Add `errors` to the `internal/prover/replay_test.go` imports, define the stub beside `fakeRunner`, and add these tests:

```go
type replayStateErrorStub struct {
	err error
}

func (s replayStateErrorStub) Error() error {
	return s.err
}

func TestReplayStateErrorWrapsDeferredError(t *testing.T) {
	sentinel := errors.New("missing trie node")
	err := replayStateError(replayStateErrorStub{err: sentinel}, "after block processing")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped state error, got %v", err)
	}
	if !strings.Contains(err.Error(), "after block processing") {
		t.Fatalf("expected replay phase in error, got %v", err)
	}
}

func TestReplayStateErrorAllowsCleanState(t *testing.T) {
	if err := replayStateError(replayStateErrorStub{}, "after intermediate root"); err != nil {
		t.Fatalf("unexpected clean state error: %v", err)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```bash
go test ./internal/prover -run '^TestReplayStateError' -count=1
```

Expected: build failure containing `undefined: replayStateError`.

- [ ] **Step 3: Add the shared guard and both phase checks**

Add the interface beside `GethRunner` in `internal/prover/replay.go`:

```go
type replayStateErrorSource interface {
	Error() error
}
```

Update the central portion of `GethRunner.Execute` so every execution variant uses the same checks:

```go
	res, err := processReplayBlock(ctx, chain, config, executionBlock, expectedDifficulty, db, vm.Config{})
	if err != nil {
		return ReplayResult{}, err
	}
	if err := replayStateError(db, "after block processing"); err != nil {
		return ReplayResult{}, err
	}
	if err := validator.ValidateState(executionBlock, db, res, true); err != nil {
		return ReplayResult{}, err
	}
	if err := validateReplayRequestsHash(executionBlock.Header(), res.Requests); err != nil {
		return ReplayResult{}, err
	}

	receiptRoot := types.DeriveSha(res.Receipts, trie.NewStackTrie(nil))
	stateRoot := db.IntermediateRoot(config.IsEIP158(executionBlock.Number()))
	if err := replayStateError(db, "after intermediate root"); err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{
		StateRoot:   stateRoot,
		ReceiptRoot: receiptRoot,
		Receipts:    res.Receipts,
	}, nil
```

Add the helper immediately after `GethRunner.Execute`:

```go
func replayStateError(source replayStateErrorSource, phase string) error {
	if err := source.Error(); err != nil {
		return fmt.Errorf("witness state error %s: %w", phase, err)
	}
	return nil
}
```

Do not add a second Unzen-only guard; `GethRunner.Execute` already covers both `core.StateProcessor` and `processUnzenReplayBlock`.

- [ ] **Step 4: Format and run focused plus fixture regressions**

Run:

```bash
gofmt -w internal/prover/replay.go internal/prover/replay_test.go
go test ./internal/prover -run '^(TestReplayStateError.*|TestSharedShastaFixtureReplaysStateless)$' -count=1
```

Expected: both guard tests and `TestSharedShastaFixtureReplaysStateless` pass.

- [ ] **Step 5: Commit the replay fix**

```bash
git add internal/prover/replay.go internal/prover/replay_test.go
git commit -m "fix(prover): reject deferred replay state errors"
```

---

### Task 2: Require an explicit configured proving mode

**Files:**
- Modify: `internal/prover/signer.go:81-109`
- Test: `internal/prover/signer_test.go:85-92`

**Interfaces:**
- Consumes: `ServiceConfig.Mode` and the existing `ProvingModeNative`, `ProvingModeTEE`, and `envProvingMode` constants.
- Produces: `NewConfiguredReplayService` returns `GAIKO2_PROVING_MODE must be set to "native" or "tee"` for an empty normalized mode.

- [ ] **Step 1: Write failing empty-mode and explicit-native tests**

Add these tests before `TestNewConfiguredReplayServiceRejectsUnknownMode`:

```go
func TestNewConfiguredReplayServiceRejectsEmptyMode(t *testing.T) {
	_, err := NewConfiguredReplayService(ServiceConfig{}, nil)
	if err == nil || err.Error() != `GAIKO2_PROVING_MODE must be set to "native" or "tee"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredReplayServiceAcceptsExplicitNativeMode(t *testing.T) {
	if _, err := NewConfiguredReplayService(ServiceConfig{Mode: ProvingModeNative}, nil); err != nil {
		t.Fatalf("explicit native mode: %v", err)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify the empty-mode case fails**

Run:

```bash
go test ./internal/prover -run '^TestNewConfiguredReplayService(RejectsEmptyMode|AcceptsExplicitNativeMode)$' -count=1
```

Expected: `TestNewConfiguredReplayServiceRejectsEmptyMode` fails because the current constructor silently selects native mode; the explicit-native test passes.

- [ ] **Step 3: Replace the native fallback with a fail-closed error**

Change the beginning of `NewConfiguredReplayService` to:

```go
func NewConfiguredReplayService(cfg ServiceConfig, runner Runner) (ReplayService, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		return ReplayService{}, fmt.Errorf(
			"%s must be set to %q or %q",
			envProvingMode,
			ProvingModeNative,
			ProvingModeTEE,
		)
	}

	var signer ProofSigner
```

Leave the existing `native`, `tee`, and unsupported-mode switch cases unchanged.

- [ ] **Step 4: Format and run all signer/config tests**

Run:

```bash
gofmt -w internal/prover/signer.go internal/prover/signer_test.go
go test ./internal/prover -run '^(TestNewConfiguredReplayService|TestServiceConfigFromEnv)' -count=1
```

Expected: all configured-service and environment parsing tests pass.

- [ ] **Step 5: Commit explicit mode enforcement**

```bash
git add internal/prover/signer.go internal/prover/signer_test.go
git commit -m "fix(prover): require explicit proving mode"
```

---

### Task 3: Validate startup before listening and warn in native mode

**Files:**
- Modify: `cmd/gaiko2/main.go:114-155`
- Modify: `cmd/gaiko2/main_test.go:24-171`
- Modify: `README.md:48-81`

**Interfaces:**
- Consumes: the Task 2 empty-mode error from `NewConfiguredReplayService`.
- Produces: `normalizedProvingMode(mode string) string` with normalization only, plus the exact native warning prefix `WARNING: native proving mode uses a public deterministic signing key`.

- [ ] **Step 1: Update server tests for explicit mode and add failing startup-order tests**

Insert the following exact statement as the first setup line in each of
`TestRunServerPrintsListeningAddress`, `TestRunServerUsesPortFromEnv`,
`TestRunServerRejectsV1WithoutGuestInput`, and
`TestRunServerShutsDownGracefullyOnContextCancel` so every successful server
path declares its mode:

```go
setEnv(t, "GAIKO2_PROVING_MODE", "native")
```

Extend `TestRunServerPrintsStartupSummary` with:

```go
	if !strings.Contains(output, "WARNING: native proving mode uses a public deterministic signing key") ||
		!strings.Contains(output, "never register") {
		t.Fatalf("expected native safety warning, got %q", output)
	}
```

Add this new test after the startup-summary test:

```go
func TestRunServerRejectsUnsetModeBeforeStartupOrListen(t *testing.T) {
	prevListen := listenFn
	t.Cleanup(func() {
		listenFn = prevListen
	})

	setEnv(t, "GAIKO2_PROVING_MODE", "")
	listenCalled := false
	listenFn = func(network, addr string) (net.Listener, error) {
		listenCalled = true
		return fakeListener{addr: fakeAddr("127.0.0.1:18080")}, nil
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != `GAIKO2_PROVING_MODE must be set to "native" or "tee"` {
		t.Fatalf("unexpected error: %v", err)
	}
	if listenCalled {
		t.Fatalf("server listened before validating proving mode")
	}
	if strings.Contains(stdout.String(), "starting gaiko2 provider") {
		t.Fatalf("server printed a successful startup summary before validation: %q", stdout.String())
	}
}
```

- [ ] **Step 2: Run focused server tests and verify the new assertions fail**

Run:

```bash
go test ./cmd/gaiko2 -run '^TestRunServer' -count=1
```

Expected: the native warning assertion fails, and the unset-mode test reports that a startup summary was printed before validation.

- [ ] **Step 3: Reorder startup, print the native warning, and remove mode fallback normalization**

Replace the server branch in `run` from configuration loading through listener creation with:

```go
		cfg, err := prover.ServiceConfigFromEnv()
		if err != nil {
			return err
		}
		mode := normalizedProvingMode(cfg.Mode)
		service, err := newReplayServiceFn(cfg, nil)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(
			stdout,
			"starting gaiko2 provider mode=%s tee_type=%s fork=%s instance_id=%d config_dir=%s secret_dir=%s listen=%s\n",
			mode,
			strings.TrimSpace(cfg.TeeType),
			strings.TrimSpace(cfg.Fork),
			cfg.InstanceID,
			cfg.ConfigDir,
			cfg.SecretDir,
			addr,
		)
		if mode == prover.ProvingModeNative {
			_, _ = fmt.Fprintln(
				stdout,
				"WARNING: native proving mode uses a public deterministic signing key; use it only for local/development testing and never register its signer in a verifier protecting real value",
			)
		}
		listener, err := listenFn("tcp", addr)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "listening on %s\n", formatListeningAddr(listener.Addr()))
		return serveFn(ctx, listener, api.NewServer(service))
```

Reduce `normalizedProvingMode` to normalization only:

```go
func normalizedProvingMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}
```

- [ ] **Step 4: Update README startup instructions and native trust warning**

Change the quick-start command to:

```bash
cd gaiko2
GAIKO2_PROVING_MODE=native go run ./cmd/gaiko2 server
```

Change `Optional proving configuration:` to `Proving configuration:` and make the first bullet:

```markdown
- `GAIKO2_PROVING_MODE=native|tee` (required)
```

Replace the sentence claiming an unset mode defaults to native with:

```markdown
`GAIKO2_PROVING_MODE` must be set explicitly. `native` mode preserves the
deterministic local-development proof flow, but its signing key is public. Never
register the native signer in a verifier protecting real value. Production
deployments must use `tee` mode with an enclave-managed key.
```

- [ ] **Step 5: Format and run CLI plus signer regressions**

Run:

```bash
gofmt -w cmd/gaiko2/main.go cmd/gaiko2/main_test.go
go test ./cmd/gaiko2 ./internal/prover -run '^(TestRunServer|TestNewConfiguredReplayService)' -count=1
git diff --check
```

Expected: all selected tests pass and `git diff --check` prints nothing.

- [ ] **Step 6: Commit startup hardening and user documentation**

```bash
git add cmd/gaiko2/main.go cmd/gaiko2/main_test.go README.md
git commit -m "fix(cmd): fail closed on unset proving mode"
```

---

### Task 4: Correct the audit classifications and recommendations

**Files:**
- Modify: `docs/audits/kimi-k3-audit.md:9-157`

**Interfaces:**
- Consumes: the runtime behavior delivered by Tasks 1-3 and the approved assessment in the design spec.
- Produces: an audit whose summary table, finding bodies, and priorities distinguish fixed code gaps, operational hazards, informational notes, and refuted claims.

- [ ] **Step 1: Capture the stale claims before editing**

Run:

```bash
rg -n 'anchorBlockHash|GAIKO2_DEV_MODE|Refuse to serve|split the proof-signing key' docs/audits/kimi-k3-audit.md
```

Expected: matches for the incorrect Finding 7 field name, native aggregate/dev-flag recommendation, and key-splitting priority.

- [ ] **Step 2: Replace the executive assessment and status table**

Replace the current executive classification paragraph and table with:

```markdown
The review identified one real replay fail-closed gap and one unsafe startup
default. Both are fixed by the remediation described below. The published
native signer remains an intentional local/development facility: registering
that signer in a verifier protecting real value is a critical operator
misconfiguration, not a vulnerability in the aggregate HTTP endpoint. The
remaining observations are informational parity or defense-in-depth notes, and
Finding 7 is refuted because it misidentified the `Anchored` event field.

| # | Title | Severity | Validated status |
|---|-------|----------|------------------|
| 1 | Replay did not independently reject deferred `StateDB` errors | High | Fixed |
| 2 | Published native mock signer is unsafe if registered on-chain | Critical when misconfigured | Operational hazard; not an endpoint bug |
| 3 | Native mode reuses the public GoldenTouch mock key | Informational | Accepted deterministic dev-only design |
| 4 | Unset `GAIKO2_PROVING_MODE` selected native mode | Medium | Fixed |
| 5 | First witness supplies verifier/chain-spec metadata | Informational | Hardening note; attack refuted |
| 6 | zk-gas and transaction filtering require producer parity | Informational / latent | Current implementations match; review future upgrades |
| 7 | Third `Anchored` word was described as an anchor block hash | None | Refuted; the field is `ancestorsHash` |
| 8 | `u48Word` masks values after validated decode boundaries | Informational | Unreachable through validated signing paths |
| 9 | `witness.accounts` is retained but not consumed by gaiko2 | Informational | Redundant wire field; witness trie remains authoritative |
```

- [ ] **Step 3: Rewrite Findings 1-4 with the validated impact and remediation**

Replace each complete Finding 1-4 section, from its heading through the text
before the next `---`, with the corresponding exact section below. Preserve the
existing `Files` line only where it appears in the replacement.

```markdown
### Finding 1 (High): Replay did not independently reject deferred `StateDB` errors

**Files:** `internal/prover/replay.go` (`GethRunner.Execute`),
`internal/prover/l2_state.go`, `internal/prover/manifest_tx_filter.go`

**Validated impact.** The missing guard is real because stateless
`ValidateState` returns before surfacing `StateDB.Error()`. GuestInput manifest
filtering already checks deferred state errors after system calls and each
transaction, so the report's generic transaction-read attack is not an
unmitigated second path in the current request flow. Replay must nevertheless
fail independently: post-execution processing, finalization, or
`IntermediateRoot` can record a deferred error, and proof soundness must not
depend on the earlier filtering pass staying perfectly equivalent.

**Remediation.** `GethRunner.Execute` now checks the deferred error immediately
after block processing and again after intermediate-root calculation. Either
error aborts before a proof can be signed.
```

```markdown
### Finding 2 (Critical if misconfigured; operational): A registered public native mock signer permits forgery

Native mode deliberately uses a published deterministic signing key for local
and development regression output. If its signer is registered in a verifier
protecting real value, anyone can sign the final on-chain aggregation digest
directly. The aggregate HTTP endpoint is therefore not the root cause and
disabling that endpoint would not mitigate a registered public key.

**Remediation.** Native mode remains available when explicitly selected and
prints a prominent development-only warning. Operators must never register its
signer in a value-bearing verifier; production deployments use an
enclave-managed TEE key.
```

```markdown
### Finding 3 (Informational): Native mode intentionally reuses the public GoldenTouch mock key

The reuse is explicit deterministic development behavior. Replacing it with a
different key committed to the repository would still publish a proof-signing
key and would not make on-chain registration safe. No runtime key change is
made; the native trust boundary is documented and warned at startup.
```

```markdown
### Finding 4 (Medium): Unset proving mode silently selected native mode

An unset `GAIKO2_PROVING_MODE` previously selected the public native mock
signer. Unknown values already failed closed, but omitting the variable could
silently weaken an intended TEE deployment.

**Remediation.** An empty mode now fails service construction before startup
success output or listener creation. Explicit `GAIKO2_PROVING_MODE=native`
continues to provide the existing development workflow; no second dev flag is
required.
```

- [ ] **Step 4: Reclassify informational Findings 5, 6, 8, and 9**

Replace the prose after each named finding's `Files` line, through the next
`---`, with the corresponding exact assessment below. This removes the old
runtime recommendations that the validated design rejected.

```markdown
**Assessment for Finding 5:** The verifier is part of the signed digest, an
unauthorized verifier is rejected on-chain, and replay selects a hard-coded
chain configuration from the carry chain ID. This remains a metadata
consistency hardening note, not a demonstrated proof-forgery path.
```

```markdown
**Assessment for Finding 6:** The inspected gaiko2, pinned taiko-geth, and
alethia-reth implementations currently agree. This is a dependency-upgrade
review requirement rather than a present bug; future schedule or classifier
changes require parity review and canonical fixture tests.
```

```markdown
**Assessment for Finding 8:** All reachable signing paths validate the uint48
width before hashing, including aggregate validation. The mask is redundant
defense-in-depth and is not changed in this remediation.
```

```markdown
**Assessment for Finding 9:** gaiko2 executes against the authenticated witness
trie, which already supplies account state and enforces the anchor nonce during
execution. Treating the duplicate raiko2 account map as a second authority
would not improve soundness; it remains pinned for wire-input integrity.
```

- [ ] **Step 5: Replace Finding 7 and the priority list**

Replace Finding 7 with:

```markdown
### Finding 7 (Refuted): The third `Anchored` word is `ancestorsHash`

**Files:** `internal/prover/replay.go:465-501`,
`packages/protocol/contracts/layer2/core/Anchor.sol:78,135-137`

`Anchored(uint48 prevAnchorBlockNumber, uint48 anchorBlockNumber, bytes32
ancestorsHash)` does not emit the `anchorV4` checkpoint block hash in its third
word. Cross-checking that word against checkpoint calldata would reject valid
blocks. gaiko2 correctly uses the first two event words for anchor-number
continuity and validates checkpoint block hashes independently through
`validateAnchorL1Linkage`. No code change is required.
```

Replace the suggested priority list with:

```markdown
1. **Finding 1** — fixed by rejecting deferred replay state errors after block processing and root calculation.
2. **Finding 4** — fixed by requiring explicit mode selection; native mode now emits a development-only warning.
3. **Finding 6** — retain parity review and canonical fixture testing whenever taiko-geth or the canonical producer changes.
4. **Findings 2, 3, 5, 8, and 9** — retain the documented operational or informational constraints; no additional runtime change is justified by this audit.
5. **Finding 7** — refuted; do not implement the proposed cross-check.
```

- [ ] **Step 6: Verify stale recommendations are gone and the corrected facts are present**

Run:

```bash
if rg -n 'anchorBlockHash|GAIKO2_DEV_MODE|Refuse to serve|split the proof-signing key|defaults to .*native' docs/audits/kimi-k3-audit.md README.md; then exit 1; fi
rg -n 'ancestorsHash|Operational hazard; not an endpoint bug|Explicit `GAIKO2_PROVING_MODE=native`|Finding 7 \(Refuted\)' docs/audits/kimi-k3-audit.md
git diff --check
```

Expected: the first command prints nothing and exits successfully; the second finds all corrected classifications; `git diff --check` prints nothing.

- [ ] **Step 7: Commit the corrected audit**

```bash
git add docs/audits/kimi-k3-audit.md
git commit -m "docs: correct kimi-k3 audit classifications"
```

---

### Task 5: Run the complete verification gate

**Files:**
- Verify only; no files should change.

**Interfaces:**
- Consumes: all deliverables from Tasks 1-4.
- Produces: a clean branch whose complete test, vet, and diff gates pass.

- [ ] **Step 1: Run the uncached full test suite**

```bash
go test ./... -count=1
```

Expected: every package reports `ok` and the command exits zero.

- [ ] **Step 2: Run static analysis**

```bash
go vet ./...
```

Expected: no output and exit code zero.

- [ ] **Step 3: Check every committed change for whitespace errors**

```bash
git diff --check origin/main...HEAD
```

Expected: no output and exit code zero.

- [ ] **Step 4: Confirm the final branch and commit sequence**

```bash
git status --short --branch
git log --oneline --decorate origin/main..HEAD
```

Expected: the status has no changed-file lines, and the log contains the design, implementation-plan, replay-guard, explicit-mode, CLI-warning, and audit-correction commits.
