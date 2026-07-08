# Shasta Header Fork Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject non-canonical Shasta replay block headers whose fork-specific hash-affecting fields would be rejected by Taiko derivation or taiko-geth before gaiko2 signs a proof.

**Architecture:** Keep decoding as format-only parsing and add fork-aware semantic validation in manifest binding, next to existing base-fee and difficulty validation. Update the manifest fixture to emit canonical Unzen fields by default, then add focused regressions that remove or mutate one field at a time.

**Tech Stack:** Go 1.24, taiko-geth via the `github.com/ethereum/go-ethereum` module replacement, standard `testing`, existing `internal/prover` manifest fixtures.

---

## File Structure

- Modify `internal/prover/manifest_validate.go`: add `validateManifestHeaderForkFields`, call it from `validateManifestBlockBinding`, and keep all fork checks in one helper.
- Modify `internal/prover/manifest_validate_test.go`: add optional fork-field fixture fields, render those fields into block JSON, add pointer helper for hashes, and add regression tests for pre-Unzen, Unzen, and slot-number behavior.
- No production files outside `internal/prover/manifest_validate.go` should change.
- No request schema, decoder, replay runner, signer, Docker, or API files should change.

## Task 1: Add Failing Regression Tests And Fixture Support

**Files:**
- Modify: `internal/prover/manifest_validate_test.go`

- [ ] **Step 1: Add fork-field fixture fields**

Add these fields to `manifestBindingFixture` after `blockBaseFee uint64`:

```go
	blockBlobGasUsed      *uint64
	blockExcessBlobGas    *uint64
	blockParentBeaconRoot *common.Hash
	blockRequestsHash     *common.Hash
	blockSlotNumber       *uint64
```

- [ ] **Step 2: Add canonical Unzen defaults to the fixture**

In `newManifestBindingFixture`, add these values to the struct literal after `blockBaseFee: manifestTestBaseFee,`:

```go
		blockBlobGasUsed:      manifestUint64Ptr(0),
		blockExcessBlobGas:    manifestUint64Ptr(0),
		blockParentBeaconRoot: manifestHashPtr(common.Hash{}),
		blockRequestsHash:     manifestHashPtr(types.EmptyRequestsHash),
```

This is needed because the default fixture uses `params.TaikoInternalNetworkID`, and `chainConfigFor` enables Unzen from timestamp `0` for that chain.

- [ ] **Step 3: Render optional fork fields into block JSON**

In `blockJSON`, after `baseFeeJSON` is computed, add:

```go
	optionalHeaderFields := []string{}
	if f.blockBlobGasUsed != nil {
		optionalHeaderFields = append(optionalHeaderFields, fmt.Sprintf(`"blobGasUsed": "0x%x"`, *f.blockBlobGasUsed))
	}
	if f.blockExcessBlobGas != nil {
		optionalHeaderFields = append(optionalHeaderFields, fmt.Sprintf(`"excessBlobGas": "0x%x"`, *f.blockExcessBlobGas))
	}
	if f.blockParentBeaconRoot != nil {
		optionalHeaderFields = append(optionalHeaderFields, fmt.Sprintf(`"parentBeaconBlockRoot": %q`, f.blockParentBeaconRoot.Hex()))
	}
	if f.blockRequestsHash != nil {
		optionalHeaderFields = append(optionalHeaderFields, fmt.Sprintf(`"requestsHash": %q`, f.blockRequestsHash.Hex()))
	}
	if f.blockSlotNumber != nil {
		optionalHeaderFields = append(optionalHeaderFields, fmt.Sprintf(`"slotNumber": "0x%x"`, *f.blockSlotNumber))
	}
	optionalHeaderJSON := ""
	if len(optionalHeaderFields) > 0 {
		optionalHeaderJSON = ",\n\t\t" + strings.Join(optionalHeaderFields, ",\n\t\t")
	}
```

Then change the last field in the `header` format template from:

```go
		"baseFeePerGas": %s
	}`
```

to:

```go
		"baseFeePerGas": %s%s
	}`
```

and pass `optionalHeaderJSON` as the final `fmt.Sprintf` argument:

```go
		baseFeeJSON,
		optionalHeaderJSON,
```

- [ ] **Step 4: Add hash pointer helper**

Add this helper near `manifestUint64Ptr`:

```go
func manifestHashPtr(v common.Hash) *common.Hash { return &v }
```

- [ ] **Step 5: Add pre-Unzen fixture helper**

Add this helper near the other manifest fixture helpers:

```go
func configureMainnetPreUnzenManifestFixture(t *testing.T, fixture *manifestBindingFixture) {
	t.Helper()

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
	fixture.blockBlobGasUsed = nil
	fixture.blockExcessBlobGas = nil
	fixture.blockParentBeaconRoot = nil
	fixture.blockRequestsHash = nil
	fixture.blockSlotNumber = nil
}
```

- [ ] **Step 6: Add pre-Unzen regression test**

Add this test near `TestValidateManifestBindingRejectsNonzeroPreUnzenDifficulty`:

```go
func TestValidateManifestBindingRejectsPreUnzenForkHeaderFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*manifestBindingFixture)
		want   string
	}{
		{
			name: "blob gas used present",
			mutate: func(f *manifestBindingFixture) {
				f.blockBlobGasUsed = manifestUint64Ptr(0)
			},
			want: "pre-Unzen blob_gas_used must be absent",
		},
		{
			name: "excess blob gas present",
			mutate: func(f *manifestBindingFixture) {
				f.blockExcessBlobGas = manifestUint64Ptr(0)
			},
			want: "pre-Unzen excess_blob_gas must be absent",
		},
		{
			name: "parent beacon root present",
			mutate: func(f *manifestBindingFixture) {
				f.blockParentBeaconRoot = manifestHashPtr(common.Hash{})
			},
			want: "pre-Unzen parent_beacon_block_root must be absent",
		},
		{
			name: "requests hash present",
			mutate: func(f *manifestBindingFixture) {
				f.blockRequestsHash = manifestHashPtr(types.EmptyRequestsHash)
			},
			want: "pre-Unzen requests_hash must be absent",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newManifestBindingFixture(t)
			configureMainnetPreUnzenManifestFixture(t, fixture)
			tc.mutate(fixture)

			err := ValidateGuestInputManifestBinding(fixture.view(t))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
```

- [ ] **Step 7: Add Unzen regression test**

Add this test near the pre-Unzen test:

```go
func TestValidateManifestBindingRejectsInvalidUnzenForkHeaderFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*manifestBindingFixture)
		want   string
	}{
		{
			name: "missing blob gas used",
			mutate: func(f *manifestBindingFixture) {
				f.blockBlobGasUsed = nil
			},
			want: "Unzen blob_gas_used missing",
		},
		{
			name: "nonzero blob gas used",
			mutate: func(f *manifestBindingFixture) {
				f.blockBlobGasUsed = manifestUint64Ptr(1)
			},
			want: "Unzen blob_gas_used mismatch",
		},
		{
			name: "missing excess blob gas",
			mutate: func(f *manifestBindingFixture) {
				f.blockExcessBlobGas = nil
			},
			want: "Unzen excess_blob_gas missing",
		},
		{
			name: "nonzero excess blob gas",
			mutate: func(f *manifestBindingFixture) {
				f.blockExcessBlobGas = manifestUint64Ptr(1)
			},
			want: "Unzen excess_blob_gas mismatch",
		},
		{
			name: "missing parent beacon root",
			mutate: func(f *manifestBindingFixture) {
				f.blockParentBeaconRoot = nil
			},
			want: "Unzen parent_beacon_block_root missing",
		},
		{
			name: "nonzero parent beacon root",
			mutate: func(f *manifestBindingFixture) {
				f.blockParentBeaconRoot = manifestHashPtr(common.HexToHash(testHash("13")))
			},
			want: "Unzen parent_beacon_block_root mismatch",
		},
		{
			name: "missing requests hash",
			mutate: func(f *manifestBindingFixture) {
				f.blockRequestsHash = nil
			},
			want: "Unzen requests_hash missing",
		},
		{
			name: "non-empty requests hash",
			mutate: func(f *manifestBindingFixture) {
				f.blockRequestsHash = manifestHashPtr(common.HexToHash(testHash("14")))
			},
			want: "Unzen requests_hash mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newManifestBindingFixture(t)
			tc.mutate(fixture)

			err := ValidateGuestInputManifestBinding(fixture.view(t))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
```

- [ ] **Step 8: Add slot-number tests**

Add this test near the other fork-field tests:

```go
func TestValidateManifestBindingRejectsPreAmsterdamSlotNumber(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.blockSlotNumber = manifestUint64Ptr(7)

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil || !strings.Contains(err.Error(), "pre-Amsterdam slot_number must be absent") {
		t.Fatalf("expected pre-Amsterdam slot number rejection, got %v", err)
	}
}
```

Add this helper-level test near the same section:

```go
func TestValidateManifestHeaderSlotNumberRequiresAmsterdamPresence(t *testing.T) {
	cfg := cloneChainConfig(params.TaikoChainConfig)
	cfg.LondonBlock = common.Big0
	cfg.AmsterdamTime = manifestUint64Ptr(0)
	header := &types.Header{
		Number: big.NewInt(1),
		Time:   1,
	}

	err := validateManifestHeaderSlotNumber(cfg, header)
	if err == nil || !strings.Contains(err.Error(), "Amsterdam slot_number missing") {
		t.Fatalf("expected Amsterdam slot number rejection, got %v", err)
	}
}
```

This helper-level test is necessary because no supported `chainConfigFor` chain in the current taiko-geth dependency assigns `AmsterdamTime`.

- [ ] **Step 9: Run the new tests and verify they fail for the missing validator**

Run:

```bash
go test ./internal/prover -run 'TestValidateManifestBindingRejects(PreUnzenForkHeaderFields|InvalidUnzenForkHeaderFields|PreAmsterdamSlotNumber)|TestValidateManifestHeaderSlotNumberRequiresAmsterdamPresence' -count=1
```

Expected: FAIL. The new tests should fail because `validateManifestHeaderForkFields` and `validateManifestHeaderSlotNumber` do not exist yet, or because the manifest path has not yet invoked the fork-field validator.

## Task 2: Implement Fork-Aware Header Validation

**Files:**
- Modify: `internal/prover/manifest_validate.go`
- Test: `internal/prover/manifest_validate_test.go`

- [ ] **Step 1: Call the new validator from manifest binding**

In `validateManifestBlockBinding`, after `validateManifestHeaderDifficulty` succeeds and before timestamp/coinbase/gas-limit checks, add:

```go
	if err := validateManifestHeaderForkFields(view.GuestInputChainID, header); err != nil {
		return anchorV4CheckpointView{}, err
	}
```

- [ ] **Step 2: Add `validateManifestHeaderForkFields`**

Add this function after `validateManifestHeaderDifficulty`:

```go
func validateManifestHeaderForkFields(chainID uint64, header *types.Header) error {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return err
	}
	if header.Number == nil {
		return fmt.Errorf("block header is missing number for fork field validation")
	}
	if config.IsUnzen(header.Time) {
		if err := validateManifestUnzenHeaderFields(header); err != nil {
			return err
		}
	} else if err := validateManifestPreUnzenHeaderFields(header); err != nil {
		return err
	}
	return validateManifestHeaderSlotNumber(config, header)
}
```

- [ ] **Step 3: Add pre-Unzen and Unzen field helpers**

Add these functions after `validateManifestHeaderForkFields`:

```go
func validateManifestPreUnzenHeaderFields(header *types.Header) error {
	if header.BlobGasUsed != nil {
		return fmt.Errorf("pre-Unzen blob_gas_used must be absent")
	}
	if header.ExcessBlobGas != nil {
		return fmt.Errorf("pre-Unzen excess_blob_gas must be absent")
	}
	if header.ParentBeaconRoot != nil {
		return fmt.Errorf("pre-Unzen parent_beacon_block_root must be absent")
	}
	if header.RequestsHash != nil {
		return fmt.Errorf("pre-Unzen requests_hash must be absent")
	}
	return nil
}

func validateManifestUnzenHeaderFields(header *types.Header) error {
	if header.BlobGasUsed == nil {
		return fmt.Errorf("Unzen blob_gas_used missing")
	}
	if *header.BlobGasUsed != 0 {
		return fmt.Errorf("Unzen blob_gas_used mismatch: expected 0 got %d", *header.BlobGasUsed)
	}
	if header.ExcessBlobGas == nil {
		return fmt.Errorf("Unzen excess_blob_gas missing")
	}
	if *header.ExcessBlobGas != 0 {
		return fmt.Errorf("Unzen excess_blob_gas mismatch: expected 0 got %d", *header.ExcessBlobGas)
	}
	if header.ParentBeaconRoot == nil {
		return fmt.Errorf("Unzen parent_beacon_block_root missing")
	}
	if *header.ParentBeaconRoot != (common.Hash{}) {
		return fmt.Errorf("Unzen parent_beacon_block_root mismatch: expected %s got %s", common.Hash{}.Hex(), header.ParentBeaconRoot.Hex())
	}
	if header.RequestsHash == nil {
		return fmt.Errorf("Unzen requests_hash missing")
	}
	if *header.RequestsHash != types.EmptyRequestsHash {
		return fmt.Errorf("Unzen requests_hash mismatch: expected %s got %s", types.EmptyRequestsHash.Hex(), header.RequestsHash.Hex())
	}
	return nil
}
```

- [ ] **Step 4: Add slot-number helper**

Add this function after the Unzen helpers:

```go
func validateManifestHeaderSlotNumber(config *params.ChainConfig, header *types.Header) error {
	if config.IsAmsterdam(header.Number, header.Time) {
		if header.SlotNumber == nil {
			return fmt.Errorf("Amsterdam slot_number missing")
		}
		return nil
	}
	if header.SlotNumber != nil {
		return fmt.Errorf("pre-Amsterdam slot_number must be absent")
	}
	return nil
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/prover -run 'TestValidateManifestBindingRejects(PreUnzenForkHeaderFields|InvalidUnzenForkHeaderFields|PreAmsterdamSlotNumber)|TestValidateManifestHeaderSlotNumberRequiresAmsterdamPresence' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run the full prover package**

Run:

```bash
go test ./internal/prover -count=1
```

Expected: PASS. If a pre-existing test fixture fails because the default internal-chain block lacks Unzen fields, confirm the default fixture still sets the four canonical Unzen fields and that only tests intentionally configured as pre-Unzen nil them out.

- [ ] **Step 7: Commit implementation**

Run:

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "fix: validate shasta fork header fields"
```

## Task 3: Verify, Push, And Open PR

**Files:**
- Verify: all modified files

- [ ] **Step 1: Format Go files**

Run:

```bash
gofmt -w internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
```

Expected: no command output.

- [ ] **Step 2: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS for every package.

- [ ] **Step 3: Inspect final diff**

Run:

```bash
git diff --stat
git diff -- internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git status --short --branch
```

Expected: only the fork validation implementation, tests, and committed superpowers docs are present.

- [ ] **Step 4: Commit any remaining formatted changes**

If `gofmt` changed files after Task 2's commit, run:

```bash
git add internal/prover/manifest_validate.go internal/prover/manifest_validate_test.go
git commit -m "test: cover shasta fork header validation"
```

Expected: a commit is created only if there are remaining unstaged changes.

- [ ] **Step 5: Push branch**

Run:

```bash
git push -u origin codex/fix-shasta-header-fork-validation
```

Expected: branch pushes successfully.

- [ ] **Step 6: Open pull request**

Run:

```bash
gh pr create --title "fix: validate Shasta fork header fields" --body "## Summary
- validate Shasta block header fork fields before replay proof signing
- reject pre-Unzen optional blob/beacon/request fields and require canonical Unzen values
- add regressions for pre-Unzen, Unzen, and slot-number validation

## Tests
- go test ./internal/prover -count=1
- go test ./...
"
```

Expected: GitHub returns a PR URL.

## Self-Review

- Spec coverage: Task 2 implements fork-aware validation for `BlobGasUsed`, `ExcessBlobGas`, `ParentBeaconRoot`, `RequestsHash`, and `SlotNumber`; Task 1 covers the requested regression matrix; Task 3 covers verification and PR creation.
- Placeholder scan: the plan contains no deferred implementation markers and every code-changing step includes concrete code.
- Type consistency: helper names are `validateManifestHeaderForkFields`, `validateManifestPreUnzenHeaderFields`, `validateManifestUnzenHeaderFields`, and `validateManifestHeaderSlotNumber`; test code calls those names consistently.
