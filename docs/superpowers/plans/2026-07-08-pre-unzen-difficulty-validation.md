# Pre-Unzen Difficulty Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject Shasta replay blocks with nonzero pre-Unzen difficulty before gaiko2 signs proof-carry checkpoint block hashes.

**Architecture:** Add a narrow manifest header invariant in `validateManifestBlockBinding`, using the canonical chain config and the replay block timestamp to decide whether Unzen rules are active. Extend the existing manifest binding fixture so one regression test can render a nonzero block difficulty and recompute proof-carry data through the normal fixture path.

**Tech Stack:** Go, go-ethereum/taiko-geth block headers, existing `internal/prover` test fixtures.

---

## File Structure

- Modify `internal/prover/manifest_validate_test.go`: add a fixture field for block difficulty, render it in `blockJSON`, and add the regression test.
- Modify `internal/prover/manifest_validate.go`: add the pre-Unzen current block difficulty check in the manifest block validator.

### Task 1: Reject Non-Canonical Pre-Unzen Difficulty

**Files:**
- Modify: `internal/prover/manifest_validate_test.go`
- Modify: `internal/prover/manifest_validate.go`

- [ ] **Step 1: Add the failing regression test**

In `manifestBindingFixture`, add:

```go
blockDifficulty             *big.Int
```

In `newManifestBindingFixture`, initialize:

```go
blockDifficulty:      big.NewInt(0),
```

In `blockJSON`, replace the hardcoded difficulty field:

```go
"difficulty": "0x0",
```

with:

```go
"difficulty": "0x%x",
```

and pass `f.blockDifficulty` before `f.blockNumber` in the `fmt.Sprintf` argument list.

Add this test near the other manifest binding header mismatch tests:

```go
func TestValidateManifestBindingRejectsNonzeroPreUnzenDifficulty(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.chainID = params.TaikoMainnetNetworkID.Uint64()
	fixture.l2Contract = testTaikoL2Address(fixture.chainID)
	fixture.anchorTo = testTaikoL2Address(fixture.chainID)
	fixture.proposalTimestamp = 1_775_135_701
	fixture.grandparentHeader.Time = 1_775_135_698
	fixture.parentHeader.Time = 1_775_135_700
	fixture.parentHeader.ParentHash = fixture.grandparentHeader.Hash()
	fixture.manifestTimestamp = 1_775_135_701
	fixture.blockTimestamp = 1_775_135_701
	fixture.blockBaseFee = 10_000_000
	userTx := manifestUserTxJSONWithGasAndFeeCap(t, fixture.chainID, 0, testAddress("33"), 24_000, 20_000_000)
	fixture.manifestUserTxJSON = userTx
	fixture.blockUserTxJSON = userTx
	fixture.blockDifficulty = big.NewInt(1)

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected pre-Unzen difficulty rejection")
	}
	if !strings.Contains(err.Error(), "difficulty") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run the targeted test and verify it fails for the reported bug**

Run:

```bash
go test ./internal/prover -run TestValidateManifestBindingRejectsNonzeroPreUnzenDifficulty -count=1
```

Expected before implementation: the test fails because `ValidateGuestInputManifestBinding` returns nil.

- [ ] **Step 3: Add the minimal validator implementation**

In `validateManifestBlockBinding`, after transaction-root validation and before timestamp/coinbase/gas checks, add:

```go
	if err := validateManifestHeaderDifficulty(view.GuestInputChainID, header); err != nil {
		return anchorV4CheckpointView{}, err
	}
```

Add this helper near `validateManifestHeaderBaseFee`:

```go
func validateManifestHeaderDifficulty(chainID uint64, header *types.Header) error {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return err
	}
	if config.IsUnzen(header.Time) {
		return nil
	}
	if header.Difficulty == nil {
		return fmt.Errorf("missing difficulty in pre-Unzen block header")
	}
	if header.Difficulty.Sign() != 0 {
		return fmt.Errorf("pre-Unzen difficulty mismatch: expected 0 got %s", header.Difficulty)
	}
	return nil
}
```

- [ ] **Step 4: Run the targeted test and verify it passes**

Run:

```bash
go test ./internal/prover -run TestValidateManifestBindingRejectsNonzeroPreUnzenDifficulty -count=1
```

Expected after implementation: pass.

- [ ] **Step 5: Run relevant prover regression tests**

Run:

```bash
go test ./internal/prover -count=1
```

Expected: pass.

- [ ] **Step 6: Run the full repository test suite**

Run:

```bash
go test ./...
```

Expected: pass.

- [ ] **Step 7: Commit the implementation**

Run:

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go docs/superpowers/plans/2026-07-08-pre-unzen-difficulty-validation.md
git commit -m "fix: validate pre-unzen block difficulty"
```
