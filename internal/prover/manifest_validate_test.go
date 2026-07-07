package prover

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

const manifestTestTxPrivateKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const (
	manifestTestBaseFee       = uint64(5_000_000)
	manifestTestUserTxFeeCap  = uint64(10_000_000)
	manifestTestLowGasBalance = uint64(250_000_000_000)
)

func TestValidateManifestBindingAcceptsInlineCalldataSource(t *testing.T) {
	view := newManifestBindingFixture(t).view(t)

	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding: %v", err)
	}
}

func TestValidateManifestBindingAcceptsBlobBackedSource(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.blobBacked = true
	view := fixture.view(t)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate blob source hashes: %v", err)
	}
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate blob-backed manifest binding: %v", err)
	}
}

func TestValidateManifestBindingDerivesMixHashFromParentDifficulty(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.parentHeader.Difficulty = big.NewInt(0x11b626)
	fixture.parentHeader.MixDigest = common.HexToHash(testHash("99"))
	fixture.blockMixDigest = manifestMixHash(common.BigToHash(fixture.parentHeader.Difficulty), 42)
	view := fixture.view(t)

	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding: %v", err)
	}
}

func TestValidateManifestBindingAcceptsTxListFilteredTransactions(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	filteredByExecution := manifestUserTxJSON(t, fixture.chainID, 7, testAddress("31"))
	included := manifestUserTxJSON(t, fixture.chainID, 0, testAddress("32"))
	fixture.manifestUserTxJSONs = []json.RawMessage{filteredByExecution, included}
	fixture.blockUserTxJSONs = []json.RawMessage{included}
	view := fixture.view(t)

	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding with tx-list filtered transaction: %v", err)
	}
}

func TestValidateManifestBindingRevertsFilteredApplyErrorState(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	lowGas := manifestUserTxJSONWithGas(t, fixture.chainID, 0, testAddress("31"), 21_000)
	included := manifestUserTxJSON(t, fixture.chainID, 0, testAddress("32"))
	fixture.manifestUserTxJSONs = []json.RawMessage{lowGas, included}
	fixture.blockUserTxJSONs = []json.RawMessage{included}

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalance(t, signer, new(big.Int).SetUint64(manifestTestLowGasBalance))
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes

	view := fixture.view(t)
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding with reverted filtered transaction: %v", err)
	}
}

func TestValidateManifestBindingRevertsFilteredInsufficientBalanceState(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	tooExpensive := manifestUserTxJSONWithGas(t, fixture.chainID, 0, testAddress("31"), 1_000_000)
	included := manifestUserTxJSON(t, fixture.chainID, 0, testAddress("32"))
	fixture.manifestUserTxJSONs = []json.RawMessage{tooExpensive, included}
	fixture.blockUserTxJSONs = []json.RawMessage{included}

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalance(t, signer, new(big.Int).SetUint64(manifestTestLowGasBalance))
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes

	view := fixture.view(t)
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding with reverted insufficient-balance transaction: %v", err)
	}
}

func TestValidateManifestBindingRejectsCanonicalBodyPastZkGasTruncation(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	manifestTxJSONs := manifestSequentialUserTxJSONs(t, fixture.chainID, 450)
	fixture.manifestUserTxJSONs = manifestTxJSONs
	fixture.blockUserTxJSONs = manifestTxJSONs

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalance(t, signer, new(big.Int).SetUint64(1_000_000_000_000_000_000))
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected transaction root mismatch for body past zkGas truncation")
	}
	if !strings.Contains(err.Error(), "transaction root mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingKeepsRuntimeOutOfGasTransaction(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	loopContract := common.HexToAddress(testAddress("45"))
	runtimeOOG := manifestUserTxJSONWithGas(t, fixture.chainID, 0, loopContract.Hex(), 25_000)
	included := manifestUserTxJSON(t, fixture.chainID, 1, testAddress("32"))
	fixture.manifestUserTxJSONs = []json.RawMessage{runtimeOOG, included}
	fixture.blockUserTxJSONs = []json.RawMessage{runtimeOOG, included}

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessCodes, witnessStateRoot := witnessStateNodesWithBalanceAndCode(
		t,
		signer,
		new(big.Int).SetUint64(1_000_000_000_000),
		loopContract,
		runtimeOutOfGasLoopCode(),
	)
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes
	fixture.witnessCodes = witnessCodes

	view := fixture.view(t)
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding with runtime out-of-gas transaction: %v", err)
	}
}

func TestValidateManifestBindingRejectsDroppingRuntimeOutOfGasTransaction(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	loopContract := common.HexToAddress(testAddress("45"))
	runtimeOOG := manifestUserTxJSONWithGas(t, fixture.chainID, 0, loopContract.Hex(), 25_000)
	included := manifestUserTxJSON(t, fixture.chainID, 1, testAddress("32"))
	fixture.manifestUserTxJSONs = []json.RawMessage{runtimeOOG, included}
	fixture.blockUserTxJSONs = []json.RawMessage{included}

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessCodes, witnessStateRoot := witnessStateNodesWithBalanceAndCode(
		t,
		signer,
		new(big.Int).SetUint64(1_000_000_000_000),
		loopContract,
		runtimeOutOfGasLoopCode(),
	)
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes
	fixture.witnessCodes = witnessCodes

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected transaction root mismatch when runtime out-of-gas transaction is dropped")
	}
	if !strings.Contains(err.Error(), "transaction root mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsInvalidDerivedBaseFee(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.omitUserTx = true
	fixture.blockBaseFee = 1_000_000_000

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected invalid base fee error")
	}
	if !strings.Contains(err.Error(), "invalid baseFee") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsMissingBaseFeeBeforeFiltering(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.omitBlockBaseFee = true

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("expected clean missing base fee error, got panic: %v", recovered)
		}
	}()
	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected missing base fee error")
	}
	if !strings.Contains(err.Error(), "missing base fee") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingAllowsFirstBlockWithoutGrandparentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	userTx := manifestUserTxJSONWithGasAndFeeCap(t, fixture.chainID, 0, testAddress("33"), 24_000, params.ShastaInitialBaseFee)
	fixture.blockNumber = 1
	fixture.blockBaseFee = params.ShastaInitialBaseFee
	fixture.manifestUserTxJSON = userTx
	fixture.blockUserTxJSON = userTx
	fixture.parentHeader.Number = common.Big0
	fixture.parentHeader.ParentHash = common.Hash{}
	fixture.parentHeader.BaseFee = nil
	fixture.manifestGasLimit = fixture.parentHeader.GasLimit
	fixture.blockGasLimit = fixture.manifestGasLimit + shastaAnchorGasLimit
	fixture.grandparentHeader = nil
	fixture.omitGrandparentHeader = true
	fixture.blockMixDigest = manifestMixHash(common.BigToHash(fixture.parentHeader.Difficulty), fixture.blockNumber)

	if err := ValidateGuestInputManifestBinding(fixture.view(t)); err != nil {
		t.Fatalf("validate first block manifest binding without grandparent: %v", err)
	}
}

func TestValidateSourceAwareManifestAnchorsAcceptsForcedPrefixThenNormalCatchup(t *testing.T) {
	spans := []manifestAnchorSourceSpan{
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: false, blockCount: 1},
	}

	err := validateSourceAwareManifestAnchors(
		[]uint64{13414, 13414, 13414, 13414, 14188},
		spans,
		13414,
		14189,
		167001,
	)
	if err != nil {
		t.Fatalf("validate source-aware forced-prefix anchors: %v", err)
	}
}

func TestValidateSourceAwareManifestAnchorsAcceptsOutOfRangeStalledBypassShape(t *testing.T) {
	lastAnchor := uint64(13_414)
	origin := lastAnchor + anchorMaxOffsetForChain(167001) + 1
	spans := []manifestAnchorSourceSpan{
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: false, blockCount: 1},
	}

	err := validateSourceAwareManifestAnchors(
		[]uint64{lastAnchor, lastAnchor},
		spans,
		lastAnchor,
		origin,
		167001,
	)
	if err != nil {
		t.Fatalf("validate source-aware stalled-anchor bypass shape: %v", err)
	}
}

func TestValidateSourceAwareManifestAnchorsRejectsForcedSourceThatBumpsAnchor(t *testing.T) {
	spans := []manifestAnchorSourceSpan{
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: false, blockCount: 1},
	}

	err := validateSourceAwareManifestAnchors(
		[]uint64{14188, 14189},
		spans,
		13414,
		14189,
		167001,
	)
	if err == nil {
		t.Fatalf("expected forced source anchor bump rejection")
	}
	if !strings.Contains(err.Error(), "forced inclusion source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestAnchorNumbersRejectsNormalSourceThatDoesNotAdvance(t *testing.T) {
	manifest := shastaSourceManifest{
		Blocks: []shastaManifestBlock{{AnchorBlockNumber: 899}},
	}

	if validateManifestAnchorNumbers(manifest, 1_000, 899, false, 167001) {
		t.Fatalf("expected normal source anchor advancement rejection")
	}
}

func TestDecodeGuestInputL1HeadersReadsOriginAndAncestors(t *testing.T) {
	raw := mustRawMessage(t, `{"l1_header":{`+minimalHeaderJSON(100)+`},
		"l1_ancestor_headers":[{`+minimalHeaderJSON(99)+`},{`+minimalHeaderJSON(100)+`}]}`)
	origin, ancestors, err := decodeGuestInputL1Headers(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if origin.Number.Uint64() != 100 {
		t.Fatalf("origin number: got %d", origin.Number.Uint64())
	}
	if len(ancestors) != 2 || ancestors[1].Number.Uint64() != 100 {
		t.Fatalf("ancestors: got %d entries", len(ancestors))
	}
}

func TestValidateManifestBindingRejectsEmptyProposalSources(t *testing.T) {
	view := newManifestBindingFixture(t).view(t)
	taikoFields, err := decodeJSONObject(view.TaikoRaw)
	if err != nil {
		t.Fatalf("decode taiko: %v", err)
	}
	eventFields, err := decodeJSONObject(taikoFields["proposal_event"])
	if err != nil {
		t.Fatalf("decode proposal event: %v", err)
	}
	proposalFields, err := decodeJSONObject(eventFields["proposal"])
	if err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	proposalFields["sources"] = mustRawMessage(t, `[]`)
	eventFields["proposal"] = mustMarshalRawMessage(t, proposalFields)
	taikoFields["proposal_event"] = mustMarshalRawMessage(t, eventFields)
	taikoFields["data_sources"] = mustRawMessage(t, `[]`)
	view.TaikoRaw = mustMarshalRawMessage(t, taikoFields)
	view.Raw.Taiko = view.TaikoRaw
	view.DataSourcesRaw = nil

	err = ValidateGuestInputManifestBinding(view)
	if err == nil {
		t.Fatalf("expected empty proposal sources rejection")
	}
	if !strings.Contains(err.Error(), "proposal sources must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsMissingProposalParentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.omitProposalAncestorHeaders = true

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected missing proposal ancestor header")
	}
	if !strings.Contains(err.Error(), "missing proposal ancestor header") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsParentHeaderUnlinkedFromCarry(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)
	view.Carry.TransitionInput.ParentBlockHash = common.HexToHash(testHash("fe"))

	err := ValidateGuestInputManifestBinding(view)
	if err == nil {
		t.Fatalf("expected parent header carry mismatch")
	}
	if !strings.Contains(err.Error(), "proposal parent header hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsMissingGrandparentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.omitGrandparentHeader = true

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected missing grandparent header")
	}
	if !strings.Contains(err.Error(), "missing grandparent header") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsCompactGrandparentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)
	view.Raw.ProposalAncestorHeaders[0] = mustRawMessage(t, fmt.Sprintf(`{
		"number": %d,
		"hash": %q,
		"parent_hash": %q,
		"timestamp": %d
	}`,
		fixture.grandparentHeader.Number.Uint64(),
		fixture.grandparentHeader.Hash().Hex(),
		fixture.grandparentHeader.ParentHash.Hex(),
		fixture.grandparentHeader.Time+1,
	))

	err := ValidateGuestInputManifestBinding(view)
	if err == nil {
		t.Fatalf("expected compact grandparent header rejection")
	}
	if !strings.Contains(err.Error(), "missing full grandparent header") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsUnboundProposalParentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.parentHeader.ParentHash = common.HexToHash(testHash("91"))

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected parent header hash mismatch")
	}
	if !strings.Contains(err.Error(), "parent header hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsUnlinkedGrandparentHeader(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.grandparentHeader.Time = fixture.parentHeader.Time - 3

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected grandparent header hash mismatch")
	}
	if !strings.Contains(err.Error(), "grandparent header hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingRejectsPartialWitnessStateDuringFiltering(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalances(t, map[common.Address]*big.Int{
		signer:                                 new(big.Int).SetUint64(1_000_000_000_000_000_000),
		common.HexToAddress(testAddress("66")): new(big.Int).SetUint64(1),
	})
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = []string{rootWitnessNode(t, witnessStateNodes, witnessStateRoot)}
	view := fixture.view(t)

	err := ValidateGuestInputManifestBinding(view)
	if err == nil {
		t.Fatalf("expected partial witness state error")
	}
	if !strings.Contains(err.Error(), "witness state") {
		t.Fatalf("expected witness state error, got %v", err)
	}
}

func TestValidateManifestBindingRejectsMissingLastAnchorBlockNumber(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.lastAnchorBlockNumber = nil

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil || !strings.Contains(err.Error(), "missing taiko.prover_data.last_anchor_block_number") {
		t.Fatalf("expected missing last anchor rejection, got %v", err)
	}
}

func TestDecodeManifestPayloadRejectsOversizedDecodedPayload(t *testing.T) {
	payload := encodeCompressedManifestBytes(t, bytes.Repeat([]byte{0}, shastaMaxManifestDecodedPayload+1))

	_, err := decodeManifestPayload(payload, 0, 1)
	if err == nil {
		t.Fatalf("expected decoded payload size error")
	}
	if !strings.Contains(err.Error(), "decompressed manifest payload exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeManifestPayloadRejectsTooManyTransactionsInBlock(t *testing.T) {
	tx := decodeTestTransaction(t, manifestUserTxJSON(t, 167001, 0, testAddress("31")))
	txs := make(types.Transactions, int(shastaMaxManifestTxsPerBlock)+1)
	for index := range txs {
		txs[index] = tx
	}
	payload := encodeTestManifestPayload(t, testDerivationSourceManifest{
		Blocks: []testManifestBlock{{Transactions: txs}},
	})

	_, err := decodeManifestPayload(payload, 0, 1)
	if err == nil {
		t.Fatalf("expected transaction count error")
	}
	if !strings.Contains(err.Error(), "transaction count") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingIgnoresWitnessShastaTimestampForDerivation(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	futureTimestamp := fixture.proposalTimestamp + 100
	fixture.chainSpecHardForksJSON = fmt.Sprintf(`{"SHASTA": {"Timestamp": %d}}`, futureTimestamp)
	fixture.manifestTimestamp = futureTimestamp
	fixture.blockTimestamp = futureTimestamp
	fixture.blockCoinbase = fixture.proposer
	fixture.anchorBlockNumber = *fixture.lastAnchorBlockNumber
	fixture.omitUserTx = true

	err := ValidateGuestInputManifestBinding(fixture.view(t))
	if err == nil {
		t.Fatalf("expected canonical Shasta timestamp validation error")
	}
	if !strings.Contains(err.Error(), "timestamp mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifestBindingIgnoresWitnessUnzenActivationForSourceLimit(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.chainID = params.MasayaDevnetNetworkID.Uint64()
	fixture.l2Contract = testTaikoL2Address(fixture.chainID)
	fixture.anchorTo = testTaikoL2Address(fixture.chainID)
	fixture.proposalTimestamp = 2_000
	fixture.chainSpecHardForksJSON = `{"SHASTA": {"Block": 0}, "UNZEN": {"Timestamp": 0}}`
	fixture.manifestBlockCount = shastaDerivationSourceMaxBlocks + 1
	fixture.manifestTimestamp = fixture.parentHeader.Time + 1
	fixture.blockTimestamp = fixture.parentHeader.Time + 1
	fixture.blockCoinbase = fixture.proposer
	fixture.anchorBlockNumber = *fixture.lastAnchorBlockNumber
	fixture.omitUserTx = true

	if err := ValidateGuestInputManifestBinding(fixture.view(t)); err != nil {
		t.Fatalf("expected oversized witness-Unzen manifest to fall back to default with canonical pre-Unzen limit: %v", err)
	}
}

func TestValidateManifestBindingRejectsMismatches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*manifestBindingFixture)
		wantErr string
	}{
		{
			name: "derived block count",
			mutate: func(f *manifestBindingFixture) {
				f.addManifestBlock = true
			},
			wantErr: "derived manifest block count mismatch",
		},
		{
			name: "non-anchor transaction",
			mutate: func(f *manifestBindingFixture) {
				f.blockUserTxJSON = manifestUserTxJSON(t, f.chainID, 8, testAddress("77"))
			},
			wantErr: "transaction root mismatch",
		},
		{
			name: "missing anchor transaction",
			mutate: func(f *manifestBindingFixture) {
				f.omitAnchorTx = true
				f.omitUserTx = true
			},
			wantErr: "missing anchor transaction",
		},
		{
			name: "timestamp",
			mutate: func(f *manifestBindingFixture) {
				f.blockTimestamp++
			},
			wantErr: "timestamp mismatch",
		},
		{
			name: "coinbase",
			mutate: func(f *manifestBindingFixture) {
				f.blockCoinbase = common.HexToAddress(testAddress("99"))
			},
			wantErr: "coinbase mismatch",
		},
		{
			name: "gas limit",
			mutate: func(f *manifestBindingFixture) {
				f.blockGasLimit++
			},
			wantErr: "gas limit mismatch",
		},
		{
			name: "extra data",
			mutate: func(f *manifestBindingFixture) {
				f.blockExtra = []byte{0xaa}
			},
			wantErr: "extra_data mismatch",
		},
		{
			name: "mix hash",
			mutate: func(f *manifestBindingFixture) {
				f.blockMixDigest = common.HexToHash(testHash("98"))
			},
			wantErr: "mix_hash mismatch",
		},
		{
			name: "anchor recipient",
			mutate: func(f *manifestBindingFixture) {
				f.anchorTo = common.HexToAddress(testAddress("97"))
			},
			wantErr: "anchor transaction recipient mismatch",
		},
		{
			name: "anchor checkpoint",
			mutate: func(f *manifestBindingFixture) {
				f.anchorBlockNumber++
			},
			wantErr: "anchor checkpoint block number mismatch",
		},
		{
			// An undecodable blob is bound to the on-chain blob hash, so it degrades to the
			// default manifest (raiko2 #137) and the proposal still binds — instead of
			// hard-erroring and leaving the proposal permanently unprovable.
			name: "undecodable blob-backed source degrades to default and binds",
			mutate: func(f *manifestBindingFixture) {
				f.blobBacked = true
				f.corruptBlobEncoding = true
				f.anchorBlockNumber = 899
				f.blockCoinbase = f.proposer
				f.omitUserTx = true
			},
			wantErr: "",
		},
		{
			name: "invalid manifest metadata defaults instead of binding malicious metadata",
			mutate: func(f *manifestBindingFixture) {
				f.manifestTimestamp = f.parentHeader.Time
				f.manifestCoinbase = common.HexToAddress(testAddress("99"))
				f.anchorBlockNumber = 899
				f.blockCoinbase = f.proposer
				f.omitUserTx = true
			},
			wantErr: "",
		},
		{
			name: "single forced inclusion source without final normal source",
			mutate: func(f *manifestBindingFixture) {
				f.isForcedInclusion = true
				f.manifestTimestamp = 1
				f.manifestCoinbase = common.HexToAddress(testAddress("88"))
				f.manifestGasLimit = 10_000_000
				f.anchorBlockNumber = 899
				f.blockCoinbase = f.proposer
			},
			wantErr: "last Shasta derivation source must be a normal source",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newManifestBindingFixture(t)
			tc.mutate(fixture)
			view := fixture.view(t)

			err := ValidateGuestInputManifestBinding(view)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecodeBlobBackedSourceManifestDefaultsUndecodableBlob(t *testing.T) {
	// A blob-backed source whose blob is the correct length but fails the Shasta blob codec
	// degrades to the default manifest instead of hard-erroring, matching the driver's
	// resolve_source_manifest (raiko2 #137 / taiko-client-rs #21854). Without this, a forced
	// inclusion carrying an undecodable blob is permanently unprovable and finalization wedges.
	blob := encodeTestKonaBlob(t, []byte{0x01, 0x02, 0x03})
	blob[len(blob)-1] = 0x01 // stray non-zero byte past the encoded payload

	dataSource := blobSourceDataView{TxDataFromBlob: [][]byte{blob}}

	manifest, err := decodeBlobBackedSourceManifest(dataSource, 0, shastaDerivationSourceMaxBlocks)
	if err != nil {
		t.Fatalf("undecodable blob must degrade to the default manifest, got error: %v", err)
	}
	if !isDefaultSourceManifest(manifest) {
		t.Fatalf("expected default source manifest, got %+v", manifest)
	}
}

func TestDecodeBlobBackedSourceManifestDefaultsStrayByteBlob(t *testing.T) {
	// Mirrors the taiko-client-rs #21854 regression blob (proposal 1812 / L1 block 5167):
	// the first field element is all zeros (version 0, declared length 0) and a stray
	// non-zero byte sits past the declared length, so the blob codec rejects it.
	raw := make([]byte, shastaBytesPerBlob)
	raw[6] = 0x02

	dataSource := blobSourceDataView{TxDataFromBlob: [][]byte{raw}}

	manifest, err := decodeBlobBackedSourceManifest(dataSource, 0, shastaDerivationSourceMaxBlocks)
	if err != nil {
		t.Fatalf("stray-byte forced-inclusion blob must degrade to the default manifest, got error: %v", err)
	}
	if !isDefaultSourceManifest(manifest) {
		t.Fatalf("expected default source manifest, got %+v", manifest)
	}
}

func TestDecodeBlobBackedSourceManifestRejectsMissingBlobData(t *testing.T) {
	// Missing blob bytes are host-input corruption, not proposal content: unlike an
	// undecodable blob, this stays a hard error (driver analog: a transient fetch failure
	// that is retried, never defaulted).
	dataSource := blobSourceDataView{}

	_, err := decodeBlobBackedSourceManifest(dataSource, 0, shastaDerivationSourceMaxBlocks)
	if err == nil || !strings.Contains(err.Error(), "missing blob data") {
		t.Fatalf("missing blob data must remain a hard error, got: %v", err)
	}
}

func TestDecodeBlobBackedSourceManifestRejectsWrongBlobLength(t *testing.T) {
	// A wrong-length blob is host-input corruption, not proposal content, and stays a hard error.
	dataSource := blobSourceDataView{TxDataFromBlob: [][]byte{make([]byte, 10)}}

	_, err := decodeBlobBackedSourceManifest(dataSource, 0, shastaDerivationSourceMaxBlocks)
	if err == nil || !strings.Contains(err.Error(), "invalid length") {
		t.Fatalf("wrong-length blob must remain a hard error, got: %v", err)
	}
}

func TestDecodeBlobBackedSourceManifestRejectsWrongLengthEvenWithUndecodableBlob(t *testing.T) {
	// Blob length is validated up front for every blob, matching raiko2's collect-then-decode
	// ordering: a wrong-length blob (host corruption) stays a hard error even when an earlier
	// blob is undecodable content that would otherwise degrade to the default manifest. This
	// keeps gaiko2 and raiko2 in lockstep on which failures are retryable vs. proposal-bound.
	undecodable := make([]byte, shastaBytesPerBlob)
	undecodable[6] = 0x02
	dataSource := blobSourceDataView{TxDataFromBlob: [][]byte{undecodable, make([]byte, 10)}}

	_, err := decodeBlobBackedSourceManifest(dataSource, 0, shastaDerivationSourceMaxBlocks)
	if err == nil || !strings.Contains(err.Error(), "invalid length") {
		t.Fatalf("wrong-length blob must remain a hard error regardless of other blobs, got: %v", err)
	}
}

func TestValidateManifestBindingRejectsNonCanonicalAnchorGas(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.anchorGas = shastaAnchorGasLimit + 1
	view := fixture.view(t)
	err := ValidateGuestInputManifestBinding(view)
	if err == nil || !strings.Contains(err.Error(), "anchor transaction gas limit mismatch") {
		t.Fatalf("expected anchor gas rejection, got %v", err)
	}
}

func TestValidateManifestBindingRejectsNonGoldenTouchAnchorSender(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.anchorPrivateKeyHex = manifestTestTxPrivateKeyHex
	view := fixture.view(t)
	block, _, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	_, err = validateManifestAnchorTransaction(view, block.Transactions()[0], block.Header(), shastaManifestBlock{
		AnchorBlockNumber: fixture.anchorBlockNumber,
	})
	if err == nil || !strings.Contains(err.Error(), "anchor transaction sender mismatch") {
		t.Fatalf("expected anchor sender rejection, got %v", err)
	}
}

func TestValidateManifestBindingCollectsAnchorCheckpoints(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)
	block, _, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cp, err := validateManifestAnchorTransaction(view, block.Transactions()[0], block.Header(),
		shastaManifestBlock{AnchorBlockNumber: fixture.anchorBlockNumber})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cp.blockNumber != fixture.anchorBlockNumber {
		t.Fatalf("checkpoint blockNumber: got %d want %d", cp.blockNumber, fixture.anchorBlockNumber)
	}
	if cp.blockHash != fixture.anchorCheckpointBlockHash || cp.stateRoot != fixture.anchorCheckpointStateRoot {
		t.Fatalf("checkpoint hash/stateRoot not returned")
	}
}

func TestValidateManifestBindingRejectsRequestControlledAnchorRecipient(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	attacker := common.HexToAddress(testAddress("44"))
	fixture.l2Contract = attacker
	fixture.anchorTo = attacker
	view := fixture.view(t)
	err := ValidateGuestInputManifestBinding(view)
	if err == nil || !strings.Contains(err.Error(), "anchor transaction recipient mismatch") {
		t.Fatalf("expected canonical anchor recipient rejection, got %v", err)
	}
}

func TestValidateManifestBindingRejectsNonCanonicalAnchorSignature(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.anchorSignatureMode = anchorSignatureGoEthereum
	view := fixture.view(t)

	err := ValidateGuestInputManifestBinding(view)
	if err == nil || !strings.Contains(err.Error(), "anchor transaction signature mismatch") {
		t.Fatalf("expected canonical anchor signature rejection, got %v", err)
	}
}

func TestDecodeAnchorV4CheckpointRejectsTrailingCalldata(t *testing.T) {
	input := anchorInput(t, 900, common.HexToHash(testHash("61")), common.HexToHash(testHash("62")))
	input = append(input, 0x00)

	_, err := decodeAnchorV4Checkpoint(input)
	if err == nil || !strings.Contains(err.Error(), "anchorV4 calldata length mismatch") {
		t.Fatalf("expected trailing anchor calldata rejection, got %v", err)
	}
}

func testTaikoL2Address(chainID uint64) common.Address {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	const suffix = "10001"
	padding := common.AddressLength*2 - len(prefix) - len(suffix)
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + suffix)
}

func testCheckpointStoreAddress(chainID uint64) common.Address {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	const suffix = "5"
	padding := common.AddressLength*2 - len(prefix) - len(suffix)
	if padding < 0 {
		panic("chain ID too large for test checkpoint store address")
	}
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + suffix)
}

func TestValidateGuestInputBlobSourcesAcceptsInlineCalldataSource(t *testing.T) {
	view := newManifestBindingFixture(t).view(t)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate inline calldata source: %v", err)
	}
}

type manifestBindingFixture struct {
	chainID                     uint64
	proposalID                  uint64
	proposalTimestamp           uint64
	proposer                    common.Address
	originBlockNumber           uint64
	parentHeader                *types.Header
	grandparentHeader           *types.Header
	manifestTimestamp           uint64
	manifestCoinbase            common.Address
	manifestGasLimit            uint64
	manifestUserTxJSON          json.RawMessage
	manifestUserTxJSONs         []json.RawMessage
	blockUserTxJSON             json.RawMessage
	blockUserTxJSONs            []json.RawMessage
	blockTimestamp              uint64
	blockCoinbase               common.Address
	blockNumber                 uint64
	blockGasLimit               uint64
	blockExtra                  []byte
	blockMixDigest              common.Hash
	blockBaseFee                uint64
	omitBlockBaseFee            bool
	l2Contract                  common.Address
	anchorTo                    common.Address
	anchorGas                   uint64
	anchorPrivateKeyHex         string
	anchorSignatureMode         anchorSignatureMode
	anchorBlockNumber           uint64
	lastAnchorBlockNumber       *uint64
	omitAnchorTx                bool
	omitUserTx                  bool
	addManifestBlock            bool
	manifestBlockCount          int
	chainSpecHardForksJSON      string
	witnessStateNodes           []string
	witnessCodes                []string
	omitProposalAncestorHeaders bool
	omitGrandparentHeader       bool
	blobBacked                  bool
	corruptBlobEncoding         bool
	isForcedInclusion           bool

	// L1 anchor linkage state, built by buildL1Chain from a coherent synthetic
	// L1 chain spanning [anchorBlockNumber, originBlockNumber]. Derived so the
	// anchor checkpoint and proposal origin bind to real L1 ancestor headers.
	l1Headers                 []*types.Header
	originBlockHash           common.Hash
	anchorCheckpointBlockHash common.Hash
	anchorCheckpointStateRoot common.Hash
	omitL1Headers             bool
}

type anchorSignatureMode uint8

const (
	anchorSignatureFixedK anchorSignatureMode = iota
	anchorSignatureGoEthereum
)

func newManifestBindingFixture(t *testing.T) *manifestBindingFixture {
	t.Helper()

	chainID := uint64(167001)
	proposalID := uint64(12345)
	parentMixDigest := common.HexToHash(testHash("91"))
	parentDifficulty := big.NewInt(0x11b626)
	manifestTx := manifestUserTxJSON(t, chainID, 0, testAddress("33"))
	manifestGasLimit := uint64(30_000_000)
	blockNumber := uint64(42)
	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalance(t, signer, new(big.Int).SetUint64(1_000_000_000_000_000_000))

	fixture := &manifestBindingFixture{
		chainID:           chainID,
		proposalID:        proposalID,
		proposalTimestamp: 1_100,
		proposer:          common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		originBlockNumber: 1_000,
		grandparentHeader: &types.Header{
			ParentHash:  common.HexToHash(testHash("89")),
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.HexToAddress(testAddress("09")),
			Root:        common.HexToHash(testHash("88")),
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			Bloom:       types.Bloom{},
			Difficulty:  big.NewInt(0),
			Number:      big.NewInt(40),
			GasLimit:    31_000_000,
			GasUsed:     0,
			Time:        998,
			Extra:       []byte{},
			MixDigest:   common.HexToHash(testHash("90")),
			Nonce:       types.BlockNonce{},
			BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
		},
		parentHeader: &types.Header{
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.HexToAddress(testAddress("10")),
			Root:        witnessStateRoot,
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			Bloom:       types.Bloom{},
			Difficulty:  parentDifficulty,
			Number:      big.NewInt(41),
			GasLimit:    31_000_000,
			GasUsed:     0,
			Time:        1_000,
			Extra:       []byte{},
			MixDigest:   parentMixDigest,
			Nonce:       types.BlockNonce{},
			BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
		},
		manifestTimestamp:     1_001,
		manifestCoinbase:      common.HexToAddress(testAddress("22")),
		manifestGasLimit:      manifestGasLimit,
		manifestUserTxJSON:    manifestTx,
		blockUserTxJSON:       manifestTx,
		blockTimestamp:        1_001,
		blockCoinbase:         common.HexToAddress(testAddress("22")),
		blockNumber:           blockNumber,
		blockGasLimit:         manifestGasLimit + 1_000_000,
		blockExtra:            manifestExtraData(42, proposalID),
		blockMixDigest:        manifestMixHash(common.BigToHash(parentDifficulty), blockNumber),
		blockBaseFee:          manifestTestBaseFee,
		l2Contract:            testTaikoL2Address(chainID),
		anchorTo:              testTaikoL2Address(chainID),
		anchorGas:             shastaAnchorGasLimit,
		anchorPrivateKeyHex:   nativeProofPrivateKey,
		anchorSignatureMode:   anchorSignatureFixedK,
		anchorBlockNumber:     900,
		lastAnchorBlockNumber: manifestUint64Ptr(899),
		witnessStateNodes:     witnessStateNodes,
	}
	fixture.parentHeader.ParentHash = fixture.grandparentHeader.Hash()
	return fixture
}

// buildL1Chain synthesizes a coherent L1 chain spanning
// [anchorBlockNumber, originBlockNumber] and records the hashes the anchor
// checkpoint and proposal origin must bind to. Headers are contiguous and
// parent-linked so validateAnchorL1Linkage accepts them on the normal path.
func (f *manifestBindingFixture) buildL1Chain(t *testing.T) {
	t.Helper()

	if f.originBlockNumber < f.anchorBlockNumber {
		t.Fatalf("originBlockNumber %d below anchorBlockNumber %d", f.originBlockNumber, f.anchorBlockNumber)
	}

	headers := make([]*types.Header, 0, f.originBlockNumber-f.anchorBlockNumber+1)
	var parentHash common.Hash
	for number := f.anchorBlockNumber; number <= f.originBlockNumber; number++ {
		header := &types.Header{
			ParentHash:  parentHash,
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.HexToAddress(testAddress("0a")),
			Root:        common.HexToHash(fmt.Sprintf("0x%064x", 0x100000+number)),
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			Bloom:       types.Bloom{},
			Difficulty:  big.NewInt(0),
			Number:      new(big.Int).SetUint64(number),
			GasLimit:    30_000_000,
			GasUsed:     0,
			Time:        900_000 + number,
			Extra:       []byte{},
			MixDigest:   common.Hash{},
			Nonce:       types.BlockNonce{},
			BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
		}
		parentHash = header.Hash()
		headers = append(headers, header)
	}

	f.l1Headers = headers
	anchorHeader := headers[0]
	originHeader := headers[len(headers)-1]
	f.anchorCheckpointBlockHash = anchorHeader.Hash()
	f.anchorCheckpointStateRoot = anchorHeader.Root
	f.originBlockHash = originHeader.Hash()
}

// l1HeadersJSON renders the taiko.l1_header and taiko.l1_ancestor_headers JSON
// fragment (with leading comma) for embedding in the taiko object. The headers
// are the full eth JSON shape consumed by decodeGuestInputL1Headers.
func (f *manifestBindingFixture) l1HeadersJSON(t *testing.T) string {
	t.Helper()
	if f.omitL1Headers || len(f.l1Headers) == 0 {
		return ""
	}
	originHeader := f.l1Headers[len(f.l1Headers)-1]
	ancestors := make([]string, 0, len(f.l1Headers))
	for _, header := range f.l1Headers {
		ancestors = append(ancestors, headerJSON(t, header))
	}
	return fmt.Sprintf(`,
			"l1_header": %s,
			"l1_ancestor_headers": [%s]`,
		headerJSON(t, originHeader),
		strings.Join(ancestors, ","),
	)
}

func (f *manifestBindingFixture) view(t *testing.T) *GuestInputView {
	t.Helper()

	f.buildL1Chain(t)

	manifestPayload := f.manifestPayload(t)
	dataSourceJSON, sourceJSON := f.dataSourceAndSourceJSON(t, manifestPayload)
	proposalJSON := f.proposalJSON(t, sourceJSON)
	block := f.blockJSON(t)
	blockHash := replayBlockHash(t, block)
	parentHash := f.parentHeader.Hash().Hex()
	stateRoot := common.HexToHash(testHash("55")).Hex()
	witnessStateJSON, err := json.Marshal(f.witnessStateNodes)
	if err != nil {
		t.Fatalf("marshal witness state nodes: %v", err)
	}
	witnessCodesJSON, err := json.Marshal(f.witnessCodes)
	if err != nil {
		t.Fatalf("marshal witness codes: %v", err)
	}
	lastAnchorBlockNumberJSON := ""
	if f.lastAnchorBlockNumber != nil {
		lastAnchorBlockNumberJSON = fmt.Sprintf(`,
				"last_anchor_block_number": %d`, *f.lastAnchorBlockNumber)
	}

	proposalAncestorHeaders := []json.RawMessage{}
	if !f.omitGrandparentHeader && f.grandparentHeader != nil {
		proposalAncestorHeaders = append(proposalAncestorHeaders, mustRawMessage(t, f.witnessHeaderJSON(t, f.grandparentHeader)))
	}
	proposalAncestorHeaders = append(proposalAncestorHeaders, mustRawMessage(t, f.witnessHeaderJSON(t, f.parentHeader)))
	if f.omitProposalAncestorHeaders {
		proposalAncestorHeaders = nil
	}

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": %s,
				"witness": {"state": %s, "state_indices": [], "codes": %s, "headers": [%s]},
				"accounts": {}
			}`, block, f.chainSpecJSON(t), witnessStateJSON, witnessCodesJSON, f.parentWitnessHeaderJSON(t))),
		},
		Taiko: mustRawMessage(t, fmt.Sprintf(`{
			"chain_spec": {"chain_id": %d},
			"proposal_id": %d,
			"proposal_event": {"proposal": %s},
			"prover_data": {
				"actual_prover": %q%s
			},
			"data_sources": [%s]%s
		}`, f.chainID, f.proposalID, proposalJSON, testAddress("77"), lastAnchorBlockNumberJSON, dataSourceJSON, f.l1HeadersJSON(t))),
		ProposalAncestorHeaders: proposalAncestorHeaders,
		ProofCarryData: f.proofCarryData(
			t,
			parentHash,
			blockHash,
			stateRoot,
			proposalJSON,
		),
	}

	view, err := DecodeGuestInput(input)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}
	return view
}

func (f *manifestBindingFixture) proofCarryData(
	t *testing.T,
	parentHash string,
	blockHash string,
	stateRoot string,
	proposalRaw json.RawMessage,
) json.RawMessage {
	t.Helper()

	proposal, err := decodeShastaProposal(proposalRaw)
	if err != nil {
		t.Fatalf("decode proposal for carry: %v", err)
	}
	proposalHash, err := hashShastaProposal(proposal)
	if err != nil {
		t.Fatalf("hash proposal for carry: %v", err)
	}
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"verifier": %q,
		"transition_input": {
			"proposal_id": %d,
			"proposal_hash": %q,
			"parent_proposal_hash": %q,
			"parent_block_hash": %q,
			"actual_prover": %q,
			"transition": {
				"proposer": %q,
				"timestamp": %d
			},
			"checkpoint": {
				"blockNumber": "0x%x",
				"blockHash": %q,
				"stateRoot": %q
			}
		}
	}`,
		f.chainID,
		testAddress("f9"),
		proposal.ID,
		proposalHash.Hex(),
		proposal.ParentProposalHash.Hex(),
		parentHash,
		testAddress("77"),
		proposal.Proposer.Hex(),
		proposal.Timestamp,
		f.blockNumber,
		blockHash,
		stateRoot,
	))
}

func (f *manifestBindingFixture) dataSourceAndSourceJSON(t *testing.T, manifestPayload []byte) (string, string) {
	t.Helper()

	if !f.blobBacked {
		return fmt.Sprintf(`{"tx_data_from_calldata": %q}`, "0x"+hex.EncodeToString(manifestPayload)),
			fmt.Sprintf(`{
				"isForcedInclusion": %t,
				"blobSlice": {
					"blobHashes": [],
					"offset": 0,
					"timestamp": %d
				}
			}`, f.isForcedInclusion, f.proposalTimestamp)
	}

	blob := encodeTestKonaBlob(t, manifestPayload)
	if f.corruptBlobEncoding {
		blob[len(blob)-1] = 0x01
	}
	_, blobHash := testBlobCommitmentAndHash(t, blob)
	return fmt.Sprintf(`{"tx_data_from_blob": [%s]}`, hexStringJSON(blob)),
		fmt.Sprintf(`{
			"isForcedInclusion": %t,
			"blobSlice": {
				"blobHashes": [%q],
				"offset": 0,
				"timestamp": %d
			}
		}`, f.isForcedInclusion, blobHash, f.proposalTimestamp)
}

func (f *manifestBindingFixture) manifestPayload(t *testing.T) []byte {
	t.Helper()

	rawUserTxs := f.manifestUserTxJSONs
	if rawUserTxs == nil {
		rawUserTxs = []json.RawMessage{f.manifestUserTxJSON}
	}
	userTxs := make(types.Transactions, 0, len(rawUserTxs))
	for _, rawUserTx := range rawUserTxs {
		userTxs = append(userTxs, decodeTestTransaction(t, rawUserTx))
	}

	blockCount := 1
	if f.addManifestBlock {
		blockCount = 2
	}
	if f.manifestBlockCount > 0 {
		blockCount = f.manifestBlockCount
	}
	blocks := make([]testManifestBlock, 0, blockCount)
	for index := 0; index < blockCount; index++ {
		blocks = append(blocks, testManifestBlock{
			Timestamp:         f.manifestTimestamp + uint64(index),
			Coinbase:          f.manifestCoinbase,
			AnchorBlockNumber: 900,
			GasLimit:          f.manifestGasLimit,
			Transactions:      userTxs,
		})
	}
	return encodeTestManifestPayload(t, testDerivationSourceManifest{Blocks: blocks})
}

func (f *manifestBindingFixture) blockJSON(t *testing.T) json.RawMessage {
	t.Helper()

	txs := []json.RawMessage{}
	if !f.omitAnchorTx {
		txs = append(txs, f.anchorTxJSON(t))
	}
	if !f.omitUserTx {
		rawUserTxs := f.blockUserTxJSONs
		if rawUserTxs == nil {
			rawUserTxs = []json.RawMessage{f.blockUserTxJSON}
		}
		txs = append(txs, rawUserTxs...)
	}
	rawTxs, err := json.Marshal(txs)
	if err != nil {
		t.Fatalf("marshal transactions: %v", err)
	}

	decodedTxs := make(types.Transactions, 0, len(txs))
	for _, rawTx := range txs {
		decodedTxs = append(decodedTxs, decodeTestTransaction(t, rawTx))
	}
	txRoot := types.DeriveSha(decodedTxs, trie.NewStackTrie(nil))

	baseFeeJSON := "null"
	if !f.omitBlockBaseFee {
		baseFeeJSON = fmt.Sprintf("%q", fmt.Sprintf("0x%x", f.blockBaseFee))
	}

	header := fmt.Sprintf(`{
		"parentHash": %q,
		"sha3Uncles": %q,
		"miner": %q,
		"stateRoot": %q,
		"transactionsRoot": %q,
		"receiptsRoot": %q,
		"logsBloom": %q,
		"difficulty": "0x0",
		"number": "0x%x",
		"gasLimit": "0x%x",
		"gasUsed": "0x0",
		"timestamp": "0x%x",
		"extraData": %q,
		"mixHash": %q,
		"nonce": "0x0000000000000000",
		"baseFeePerGas": %s
	}`,
		f.parentHeader.Hash().Hex(),
		types.EmptyUncleHash.Hex(),
		f.blockCoinbase.Hex(),
		testHash("55"),
		txRoot.Hex(),
		testHash("57"),
		testBloom(),
		f.blockNumber,
		f.blockGasLimit,
		f.blockTimestamp,
		"0x"+hex.EncodeToString(f.blockExtra),
		f.blockMixDigest.Hex(),
		baseFeeJSON,
	)

	return mustRawMessage(t, fmt.Sprintf(`{
		"header": %s,
		"body": {
			"transactions": %s,
			"ommers": [],
			"withdrawals": []
		}
	}`, header, rawTxs))
}

func (f *manifestBindingFixture) anchorTxJSON(t *testing.T) json.RawMessage {
	t.Helper()
	checkpointBlockHash := f.anchorCheckpointBlockHash
	checkpointStateRoot := f.anchorCheckpointStateRoot
	if checkpointBlockHash == (common.Hash{}) && checkpointStateRoot == (common.Hash{}) {
		checkpointBlockHash = common.HexToHash(testHash("61"))
		checkpointStateRoot = common.HexToHash(testHash("62"))
	}
	input := anchorInput(t, f.anchorBlockNumber, checkpointBlockHash, checkpointStateRoot)
	key, err := crypto.HexToECDSA(f.anchorPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse anchor tx key: %v", err)
	}
	cfg, err := chainConfigFor(f.chainID)
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: new(big.Int).SetUint64(f.chainID), Nonce: 0, GasTipCap: big.NewInt(0),
		GasFeeCap: new(big.Int).SetUint64(f.blockBaseFee), Gas: f.anchorGas, To: &f.anchorTo,
		Value: big.NewInt(0), Data: input,
	})
	signer := types.MakeSigner(cfg, new(big.Int).SetUint64(f.blockNumber), f.blockTimestamp)
	var signed *types.Transaction
	switch f.anchorSignatureMode {
	case anchorSignatureFixedK:
		signed = signTestAnchorTxFixedK(t, signer, tx, f.anchorPrivateKeyHex)
	case anchorSignatureGoEthereum:
		signed, err = types.SignTx(tx, signer, key)
		if err != nil {
			t.Fatalf("sign anchor tx: %v", err)
		}
	default:
		t.Fatalf("unknown anchor signature mode %d", f.anchorSignatureMode)
	}
	v, r, s := signed.RawSignatureValues()
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x%s", "s": "0x%s", "yParity": "0x%s"},
		"transaction": {"Eip1559": {"chain_id": "0x%x", "nonce": "0x0",
			"max_priority_fee_per_gas": "0x0", "max_fee_per_gas": "0x%x", "gas": "0x%x",
			"to": %q, "value": "0x0", "input": %q, "access_list": []}}}`,
		r.Text(16), s.Text(16), v.Text(16), f.chainID, f.blockBaseFee, f.anchorGas,
		f.anchorTo.Hex(), "0x"+hex.EncodeToString(input)))
}

func signTestAnchorTxFixedK(t *testing.T, signer types.Signer, tx *types.Transaction, privateKeyHex string) *types.Transaction {
	t.Helper()

	sig := signTestAnchorPayloadFixedK(t, signer.Hash(tx).Bytes(), privateKeyHex)
	signed, err := tx.WithSignature(signer, sig)
	if err != nil {
		t.Fatalf("sign anchor tx with fixed k: %v", err)
	}
	return signed
}

func signTestAnchorPayloadFixedK(t *testing.T, hash []byte, privateKeyHex string) []byte {
	t.Helper()

	var priv secp256k1.ModNScalar
	if overflow := priv.SetByteSlice(common.FromHex(privateKeyHex)); overflow || priv.IsZero() {
		t.Fatalf("invalid anchor private key")
	}
	for _, k := range []uint32{1, 2} {
		sig, ok := signTestPayloadWithK(hash, &priv, new(secp256k1.ModNScalar).SetInt(k))
		if ok {
			return sig
		}
	}
	t.Fatalf("failed to sign anchor tx with fixed k")
	return nil
}

func signTestPayloadWithK(hash []byte, priv *secp256k1.ModNScalar, k *secp256k1.ModNScalar) ([]byte, bool) {
	var kG secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(k, &kG)
	kG.ToAffine()

	r, overflow := testFieldToModNScalar(&kG.X)
	pubKeyRecoveryCode := byte(overflow<<1) | byte(kG.Y.IsOddBit())

	kinv := new(secp256k1.ModNScalar).InverseValNonConst(k)
	partialS := new(secp256k1.ModNScalar).Mul2(priv, &r)

	var e secp256k1.ModNScalar
	e.SetByteSlice(hash)
	s := new(secp256k1.ModNScalar).Set(partialS).Add(&e).Mul(kinv)
	if s.IsZero() {
		return nil, false
	}
	if s.IsOverHalfOrder() {
		s.Negate()
		pubKeyRecoveryCode ^= 0x01
	}

	var sig [65]byte
	r.PutBytesUnchecked(sig[:32])
	s.PutBytesUnchecked(sig[32:64])
	sig[64] = pubKeyRecoveryCode
	return sig[:], true
}

func testFieldToModNScalar(v *secp256k1.FieldVal) (secp256k1.ModNScalar, uint32) {
	var buf [32]byte
	v.PutBytes(&buf)
	var s secp256k1.ModNScalar
	overflow := s.SetBytes(&buf)
	return s, overflow
}

func (f *manifestBindingFixture) chainSpecJSON(t *testing.T) json.RawMessage {
	t.Helper()
	hardForks := f.chainSpecHardForksJSON
	if hardForks == "" {
		hardForks = `{"SHASTA": {"Block": 0}}`
	}
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"l2_contract": %q,
		"hard_forks": %s,
		"verifier_address_forks": {"SHASTA": {"SgxGeth": %q}}
	}`, f.chainID, f.l2Contract.Hex(), hardForks, testAddress("f9")))
}

func (f *manifestBindingFixture) parentWitnessHeaderJSON(t *testing.T) string {
	t.Helper()
	return f.witnessHeaderJSON(t, f.parentHeader)
}

func (f *manifestBindingFixture) witnessHeaderJSON(t *testing.T, header *types.Header) string {
	t.Helper()
	headerJSON := headerJSON(t, header)
	return fmt.Sprintf(`{"header": %s, "hash": %q}`, headerJSON, header.Hash().Hex())
}

func (f *manifestBindingFixture) proposalJSON(t *testing.T, sourceJSON string) json.RawMessage {
	t.Helper()
	originBlockHash := testHash("cd")
	if f.originBlockHash != (common.Hash{}) {
		originBlockHash = f.originBlockHash.Hex()
	}
	return mustRawMessage(t, fmt.Sprintf(`{
		"id": %d,
		"timestamp": %d,
		"endOfSubmissionWindowTimestamp": %d,
		"proposer": %q,
		"parentProposalHash": %q,
		"originBlockNumber": %d,
		"originBlockHash": %q,
		"basefeeSharingPctg": 42,
		"sources": [%s]
	}`,
		f.proposalID,
		f.proposalTimestamp,
		f.proposalTimestamp+1_000,
		f.proposer.Hex(),
		testHash("ab"),
		f.originBlockNumber,
		originBlockHash,
		sourceJSON,
	))
}

func mustMarshalRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw message: %v", err)
	}
	return raw
}

type testDerivationSourceManifest struct {
	Blocks []testManifestBlock
}

type testManifestBlock struct {
	Timestamp         uint64
	Coinbase          common.Address
	AnchorBlockNumber uint64
	GasLimit          uint64
	Transactions      types.Transactions
}

func encodeTestManifestPayload(t *testing.T, manifest testDerivationSourceManifest) []byte {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(manifest)
	if err != nil {
		t.Fatalf("rlp encode manifest: %v", err)
	}
	return encodeCompressedManifestBytes(t, encoded)
}

func encodeCompressedManifestBytes(t *testing.T, encoded []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(encoded); err != nil {
		t.Fatalf("zlib write manifest: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close manifest: %v", err)
	}

	payload := make([]byte, 64, 64+compressed.Len())
	payload[31] = 1
	binary.BigEndian.PutUint64(payload[56:64], uint64(compressed.Len()))
	payload = append(payload, compressed.Bytes()...)
	return payload
}

func manifestUserTxJSON(t *testing.T, chainID uint64, nonce uint64, to string) json.RawMessage {
	t.Helper()
	return manifestUserTxJSONWithGas(t, chainID, nonce, to, 24_000)
}

func manifestUserTxJSONWithGas(t *testing.T, chainID uint64, nonce uint64, to string, gas uint64) json.RawMessage {
	t.Helper()
	return manifestUserTxJSONWithGasAndFeeCap(t, chainID, nonce, to, gas, manifestTestUserTxFeeCap)
}

func manifestUserTxJSONWithGasAndFeeCap(t *testing.T, chainID uint64, nonce uint64, to string, gas uint64, feeCap uint64) json.RawMessage {
	t.Helper()
	key, err := crypto.HexToECDSA(manifestTestTxPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse test tx key: %v", err)
	}
	toAddress := common.HexToAddress(to)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   new(big.Int).SetUint64(chainID),
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: new(big.Int).SetUint64(feeCap),
		Gas:       gas,
		To:        &toAddress,
		Value:     big.NewInt(0),
		Data:      []byte{0x12, 0x34},
	})
	signedTx, err := types.SignTx(tx, types.NewLondonSigner(new(big.Int).SetUint64(chainID)), key)
	if err != nil {
		t.Fatalf("sign test tx: %v", err)
	}
	v, r, s := signedTx.RawSignatureValues()
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x%s", "s": "0x%s", "yParity": "0x%s"},
		"transaction": {
			"Eip1559": {
				"chain_id": "0x%x",
				"nonce": "0x%x",
				"max_priority_fee_per_gas": "0x1",
				"max_fee_per_gas": "0x%x",
				"gas": "0x%x",
				"to": %q,
				"value": "0x0",
				"input": "0x1234",
				"access_list": []
			}
			}
		}`, r.Text(16), s.Text(16), v.Text(16), chainID, nonce, feeCap, gas, to))
}

func decodeTestTransaction(t *testing.T, raw json.RawMessage) *types.Transaction {
	t.Helper()
	tx, err := decodeTransaction(raw)
	if err != nil {
		t.Fatalf("decode test transaction: %v", err)
	}
	return tx
}

func anchorInput(t *testing.T, blockNumber uint64, blockHash common.Hash, stateRoot common.Hash) []byte {
	t.Helper()
	tuple, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "blockNumber", Type: "uint48"},
		{Name: "blockHash", Type: "bytes32"},
		{Name: "stateRoot", Type: "bytes32"},
	})
	if err != nil {
		t.Fatalf("anchor tuple ABI: %v", err)
	}
	args := abi.Arguments{{Type: tuple}}
	encoded, err := args.Pack(struct {
		BlockNumber *big.Int
		BlockHash   [32]byte
		StateRoot   [32]byte
	}{
		BlockNumber: new(big.Int).SetUint64(blockNumber),
		BlockHash:   blockHash,
		StateRoot:   stateRoot,
	})
	if err != nil {
		t.Fatalf("anchor calldata ABI pack: %v", err)
	}
	selector := crypto.Keccak256([]byte("anchorV4((uint48,bytes32,bytes32))"))[:4]
	return append(append([]byte{}, selector...), encoded...)
}

func manifestExtraData(basefeeSharingPctg uint8, proposalID uint64) []byte {
	var out [7]byte
	out[0] = basefeeSharingPctg
	var proposalBytes [8]byte
	binary.BigEndian.PutUint64(proposalBytes[:], proposalID)
	copy(out[1:], proposalBytes[2:])
	return out[:]
}

func manifestMixHash(parentMix common.Hash, blockNumber uint64) common.Hash {
	var blockWord [32]byte
	binary.BigEndian.PutUint64(blockWord[24:], blockNumber)
	return crypto.Keccak256Hash(append(parentMix.Bytes(), blockWord[:]...))
}

func headerJSON(t *testing.T, header *types.Header) string {
	t.Helper()
	baseFee := "null"
	if header.BaseFee != nil {
		baseFee = fmt.Sprintf("%q", "0x"+header.BaseFee.Text(16))
	}
	raw := fmt.Sprintf(`{
		"parentHash": %q,
		"sha3Uncles": %q,
		"miner": %q,
		"stateRoot": %q,
		"transactionsRoot": %q,
		"receiptsRoot": %q,
		"logsBloom": %q,
		"difficulty": "0x%s",
		"number": "0x%x",
		"gasLimit": "0x%x",
		"gasUsed": "0x%x",
		"timestamp": "0x%x",
		"extraData": %q,
		"mixHash": %q,
		"nonce": "0x%016x",
		"baseFeePerGas": %s
	}`,
		header.ParentHash.Hex(),
		header.UncleHash.Hex(),
		header.Coinbase.Hex(),
		header.Root.Hex(),
		header.TxHash.Hex(),
		header.ReceiptHash.Hex(),
		"0x"+hex.EncodeToString(header.Bloom.Bytes()),
		header.Difficulty.Text(16),
		header.Number.Uint64(),
		header.GasLimit,
		header.GasUsed,
		header.Time,
		"0x"+hex.EncodeToString(header.Extra),
		header.MixDigest.Hex(),
		binary.BigEndian.Uint64(header.Nonce[:]),
		baseFee,
	)
	return raw
}

func minimalHeaderJSON(number uint64) string {
	return fmt.Sprintf(`"number": "0x%x",
		"gasLimit": "0x0",
		"gasUsed": "0x0",
		"timestamp": "0x0",
		"difficulty": "0x0",
		"logsBloom": %q,
		"extraData": "0x",
		"parentHash": %q,
		"sha3Uncles": %q,
		"stateRoot": %q,
		"transactionsRoot": %q,
		"receiptsRoot": %q,
		"miner": %q,
		"mixHash": %q,
		"nonce": "0x0000000000000000"`,
		number,
		testBloom(),
		testHash("00"),
		testHash("00"),
		testHash("00"),
		testHash("00"),
		testHash("00"),
		testAddress("00"),
		testHash("00"),
	)
}

func encodeTestKonaBlob(t *testing.T, payload []byte) []byte {
	t.Helper()

	const (
		testBytesPerBlob       = 131072
		testBlobEncodingRounds = 1024
		testBlobMaxDataSize    = (4*31+3)*1024 - 4
	)
	if len(payload) > testBlobMaxDataSize {
		t.Fatalf("test payload too large for one blob: %d", len(payload))
	}

	blob := make([]byte, testBytesPerBlob)
	readOffset := 0
	writeOffset := 0

	write1 := func(value byte) {
		if value&0xc0 != 0 {
			t.Fatalf("test encoder invalid 6-bit value: %x", value)
		}
		if writeOffset%32 != 0 {
			t.Fatalf("test encoder write1 at offset %d", writeOffset)
		}
		blob[writeOffset] = value
		writeOffset++
	}
	write31 := func(buf [31]byte) {
		if writeOffset%32 != 1 {
			t.Fatalf("test encoder write31 at offset %d", writeOffset)
		}
		copy(blob[writeOffset:writeOffset+31], buf[:])
		writeOffset += 31
	}
	read1 := func() byte {
		if readOffset >= len(payload) {
			return 0
		}
		value := payload[readOffset]
		readOffset++
		return value
	}
	read31 := func() [31]byte {
		var out [31]byte
		if readOffset >= len(payload) {
			return out
		}
		n := copy(out[:], payload[readOffset:])
		readOffset += n
		return out
	}

	for round := 0; round < testBlobEncodingRounds; round++ {
		if readOffset >= len(payload) {
			break
		}

		var buf31 [31]byte
		if round == 0 {
			length := uint32(len(payload))
			buf31[0] = 0
			buf31[1] = byte(length >> 16)
			buf31[2] = byte(length >> 8)
			buf31[3] = byte(length)
			toCopy := min(len(payload)-readOffset, 27)
			copy(buf31[4:4+toCopy], payload[readOffset:readOffset+toCopy])
			readOffset += toCopy
		} else {
			buf31 = read31()
		}

		x := read1()
		write1(x & 0x3f)
		write31(buf31)

		buf31 = read31()
		y := read1()
		write1((y & 0x0f) | ((x & 0xc0) >> 2))
		write31(buf31)

		buf31 = read31()
		z := read1()
		write1(z & 0x3f)
		write31(buf31)

		buf31 = read31()
		write1(((z & 0xc0) >> 2) | ((y & 0xf0) >> 4))
		write31(buf31)
	}
	if readOffset < len(payload) {
		t.Fatalf("test payload did not fit into one blob")
	}
	return blob
}

func manifestTestTxSigner(t *testing.T) common.Address {
	t.Helper()

	key, err := crypto.HexToECDSA(manifestTestTxPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse manifest test tx key: %v", err)
	}
	return crypto.PubkeyToAddress(key.PublicKey)
}

func manifestUint64Ptr(v uint64) *uint64 { return &v }

func witnessStateNodesWithBalance(t *testing.T, address common.Address, balance *big.Int) ([]string, common.Hash) {
	t.Helper()
	return witnessStateNodesWithBalances(t, map[common.Address]*big.Int{address: balance})
}

func witnessStateNodesWithBalances(t *testing.T, balances map[common.Address]*big.Int) ([]string, common.Hash) {
	t.Helper()

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, nil))
	if err != nil {
		t.Fatalf("open test state: %v", err)
	}
	for address, balance := range balances {
		statedb.AddBalance(address, uint256.MustFromBig(balance), 0)
	}

	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit test state: %v", err)
	}

	stateTrie, err := trie.NewStateTrie(trie.StateTrieID(root), tdb)
	if err != nil {
		t.Fatalf("open test state trie: %v", err)
	}
	it, err := stateTrie.NodeIterator(nil)
	if err != nil {
		t.Fatalf("iterate test state trie: %v", err)
	}

	nodes := make([]string, 0, 4)
	for it.Next(true) {
		if it.Hash() == (common.Hash{}) {
			continue
		}
		blob := it.NodeBlob()
		if len(blob) == 0 {
			continue
		}
		nodes = append(nodes, "0x"+hex.EncodeToString(blob))
	}
	return nodes, root
}

func witnessStateNodesWithBalanceAndCode(
	t *testing.T,
	balanceAddress common.Address,
	balance *big.Int,
	codeAddress common.Address,
	code []byte,
) ([]string, []string, common.Hash) {
	t.Helper()

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("open test state: %v", err)
	}
	statedb.AddBalance(balanceAddress, uint256.MustFromBig(balance), 0)
	statedb.SetCode(codeAddress, code, tracing.CodeChangeUnspecified)

	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit test state: %v", err)
	}

	stateTrie, err := trie.NewStateTrie(trie.StateTrieID(root), tdb)
	if err != nil {
		t.Fatalf("open test state trie: %v", err)
	}
	it, err := stateTrie.NodeIterator(nil)
	if err != nil {
		t.Fatalf("iterate test state trie: %v", err)
	}

	nodes := make([]string, 0, 8)
	for it.Next(true) {
		if it.Hash() == (common.Hash{}) {
			continue
		}
		blob := it.NodeBlob()
		if len(blob) == 0 {
			continue
		}
		nodes = append(nodes, "0x"+hex.EncodeToString(blob))
	}
	return nodes, []string{"0x" + hex.EncodeToString(code)}, root
}

func rootWitnessNode(t *testing.T, nodes []string, root common.Hash) string {
	t.Helper()
	for _, node := range nodes {
		blob, err := hex.DecodeString(strings.TrimPrefix(node, "0x"))
		if err != nil {
			t.Fatalf("decode witness node: %v", err)
		}
		if crypto.Keccak256Hash(blob) == root {
			return node
		}
	}
	t.Fatalf("root witness node %s not found", root.Hex())
	return ""
}

func runtimeOutOfGasLoopCode() []byte {
	return []byte{0x5b, 0x60, 0x00, 0x56}
}

func loadRealFixtureView(t *testing.T) *GuestInputView {
	t.Helper()

	if _, err := os.Stat(sharedShastaFixturePath()); err != nil {
		t.Skipf("real fixture unavailable: %v", err)
	}

	req := loadSharedShastaFixture(t)
	if req.Payload.GuestInput == nil {
		t.Fatalf("real fixture missing guest_input")
	}
	view, err := DecodeGuestInput(*req.Payload.GuestInput)
	if err != nil {
		t.Fatalf("decode real fixture guest input: %v", err)
	}
	return view
}

func TestValidateManifestBindingAcceptsRealFixtureL1Linkage(t *testing.T) {
	view := loadRealFixtureView(t)
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("real fixture must pass L1 linkage: %v", err)
	}
}

func TestValidateAnchorL1LinkageRejectsForgedCheckpointStateRoot(t *testing.T) {
	view := loadRealFixtureView(t)
	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		t.Fatalf("proposal: %v", err)
	}
	origin, ancestors, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil || origin == nil || len(ancestors) == 0 {
		t.Fatalf("l1 headers: %v", err)
	}
	// one checkpoint per anchor block number present in ancestors[0], stateRoot forged
	cp := anchorV4CheckpointView{blockNumber: ancestors[0].Number.Uint64(),
		blockHash: ancestors[0].Hash(), stateRoot: common.HexToHash(testHash("ff"))}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	err = validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, ancestors[0].Number.Uint64()-1)
	if err == nil || !strings.Contains(err.Error(), "not found in taiko.l1_ancestor_headers") {
		t.Fatalf("expected forged stateRoot rejection, got %v", err)
	}
}

func TestValidateAnchorL1LinkageBypassMatchesParentCheckpoint(t *testing.T) {
	view, account, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	_ = account
	proposal := shastaProposalView{
		OriginBlockNumber: parentAnchor + 600, // > mainnet offset 512 beyond parentAnchor
		OriginBlockHash:   fixtureOriginHash(t, view),
	}
	cp := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	// origin header must be present as l1_header for the origin checks; helper sets chainID 167001.
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, parentAnchor); err != nil {
		t.Fatalf("bypass path should accept matching parent checkpoint: %v", err)
	}
	bad := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: common.HexToHash(testHash("ee"))}
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{bad}, spans, parentAnchor); err == nil {
		t.Fatalf("bypass path must reject non-matching checkpoint")
	}
}

func TestValidateAnchorL1LinkageForcedPrefixMatchesParentCheckpoint(t *testing.T) {
	view, _, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	origin, ancestors, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil || origin == nil || len(ancestors) < 2 {
		t.Fatalf("fixture l1 headers: %v", err)
	}
	// The normal (non-prefix) checkpoint binds to a real, non-origin ancestor so
	// the per-header linkage that runs after the forced prefix accepts it.
	normalAncestor := ancestors[len(ancestors)-2]
	proposal := shastaProposalView{
		OriginBlockNumber: origin.Number.Uint64(),
		OriginBlockHash:   origin.Hash(),
	}
	forced := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	normal := anchorV4CheckpointView{
		blockNumber: normalAncestor.Number.Uint64(),
		blockHash:   normalAncestor.Hash(),
		stateRoot:   normalAncestor.Root,
	}
	spans := []manifestAnchorSourceSpan{
		{isForcedInclusion: true, blockCount: 1},
		{isForcedInclusion: false, blockCount: 1},
	}
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{forced, normal}, spans, parentAnchor); err != nil {
		t.Fatalf("forced-inclusion prefix should accept matching parent checkpoint: %v", err)
	}
	// A forced-prefix checkpoint that does not equal the verified parent
	// checkpoint must be rejected before the per-header linkage.
	badForced := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: common.HexToHash(testHash("ee"))}
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{badForced, normal}, spans, parentAnchor); err == nil {
		t.Fatalf("forced-inclusion prefix must reject non-matching parent checkpoint")
	}
}

func TestValidateAnchorL1LinkageRejectsRequestControlledCheckpointStore(t *testing.T) {
	attackerStore := common.HexToAddress(testAddress("44"))
	view, _, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixtureWithAccount(t, attackerStore)
	proposal := shastaProposalView{
		OriginBlockNumber: parentAnchor + 600,
		OriginBlockHash:   fixtureOriginHash(t, view),
	}
	cp := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}

	err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, parentAnchor)
	if err == nil || !strings.Contains(err.Error(), "checkpoint_store_contract mismatch") {
		t.Fatalf("expected request-controlled checkpoint store rejection, got %v", err)
	}
}

func TestReadParentL2StorageReturnsCheckpoint(t *testing.T) {
	view, account, blockNumber, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(blockNumber)
	gotHash, err := readParentL2Storage(view, account, blockHashSlot)
	if err != nil {
		t.Fatalf("read blockHash: %v", err)
	}
	gotRoot, err := readParentL2Storage(view, account, stateRootSlot)
	if err != nil {
		t.Fatalf("read stateRoot: %v", err)
	}
	if gotHash != wantHash || gotRoot != wantRoot {
		t.Fatalf("checkpoint read mismatch: gotHash=%s wantHash=%s gotRoot=%s wantRoot=%s",
			gotHash.Hex(), wantHash.Hex(), gotRoot.Hex(), wantRoot.Hex())
	}
}

// TestReadParentL2StorageRequiresProposalStateNodes confirms the storage-trie
// nodes carried in view.Raw.ProposalStateNodes are load-bearing: dropping them
// leaves the storage trie unresolvable, so the read must surface a missing-node
// error rather than silently returning an empty (zero) slot.
func TestReadParentL2StorageRequiresProposalStateNodes(t *testing.T) {
	view, account, blockNumber, _, _ := newCheckpointStoreStateFixture(t)
	view.Raw.ProposalStateNodes = nil
	blockHashSlot, _ := shastaCheckpointStorageSlots(blockNumber)
	if _, err := readParentL2Storage(view, account, blockHashSlot); err == nil {
		t.Fatalf("expected error reading storage slot without proposal state nodes")
	}
}

// TestReadParentL2StorageRejectsForgedParentRoot proves the parent pre-state root
// used for the CheckpointStore read is bound to the committed
// TransitionInput.ParentBlockHash: a witness whose parent header carries a forged
// state root (and therefore a different block hash) is rejected by the read alone,
// so the manifest-binding path is sound without relying on the later replay step.
func TestReadParentL2StorageRejectsForgedParentRoot(t *testing.T) {
	account := common.HexToAddress(testAddress("34"))
	newParent := func(root common.Hash) *types.Header {
		return &types.Header{
			ParentHash:  common.HexToHash(testHash("01")),
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.HexToAddress(testAddress("02")),
			Root:        root,
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			Difficulty:  big.NewInt(0),
			Number:      new(big.Int).SetUint64(24862915),
			GasLimit:    30_000_000,
			Time:        1_000,
			Extra:       []byte{},
			BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
		}
	}
	// The committed parent is the header carrying the real pre-state root; the
	// witness instead supplies a header with a forged root (hence a different hash).
	committedParentHash := newParent(common.HexToHash(testHash("cd"))).Hash()
	forgedParent := newParent(common.HexToHash(testHash("ef")))
	if forgedParent.Hash() == committedParentHash {
		t.Fatal("test setup: forging the state root must change the header hash")
	}

	child := &types.Header{
		ParentHash:  forgedParent.Hash(),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress(testAddress("02")),
		Root:        common.HexToHash(testHash("55")),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(0),
		Number:      new(big.Int).SetUint64(24862916),
		GasLimit:    30_000_000,
		Time:        1_012,
		Extra:       []byte{},
		BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
	}
	witnessJSON := mustRawMessage(t, fmt.Sprintf(`{
		"block": {"header": %s, "body": {"transactions": [], "ommers": [], "withdrawals": []}},
		"chain_spec": {"chain_id": 167001, "checkpoint_store_contract": %q},
		"witness": {"state": [], "state_indices": [], "codes": [], "headers": [%s]},
		"accounts": {}
	}`, headerJSON(t, child), account.Hex(),
		fmt.Sprintf(`{"header": %s, "hash": %q}`, headerJSON(t, forgedParent), forgedParent.Hash().Hex())))

	witnessView, _, err := decodeGuestInputWitness(witnessJSON)
	if err != nil {
		t.Fatalf("decode witness: %v", err)
	}
	view := &GuestInputView{Witnesses: []GuestInputWitnessView{witnessView}}
	view.Carry.TransitionInput.ParentBlockHash = committedParentHash

	blockHashSlot, _ := shastaCheckpointStorageSlots(24862915)
	if _, err := readParentL2Storage(view, account, blockHashSlot); err == nil ||
		!strings.Contains(err.Error(), "does not match committed parent block hash") {
		t.Fatalf("expected forged parent-root rejection, got %v", err)
	}
}

// TestValidateAnchorL1LinkageRejectsUnboundWitnessParent proves the rejection
// propagates through the binding-path linkage validator (the function
// ValidateGuestInputManifestBinding calls): if the witness parent header does not
// bind to the committed parent block hash, the bypass/forced checkpoint read fails
// there, without running ReplayService.Prove.
func TestValidateAnchorL1LinkageRejectsUnboundWitnessParent(t *testing.T) {
	view, _, parentAnchor, wantHash, wantRoot := newCheckpointStoreStateFixture(t)
	proposal := shastaProposalView{
		OriginBlockNumber: parentAnchor + 600,
		OriginBlockHash:   fixtureOriginHash(t, view),
	}
	cp := anchorV4CheckpointView{blockNumber: parentAnchor, blockHash: wantHash, stateRoot: wantRoot}
	spans := []manifestAnchorSourceSpan{{isForcedInclusion: false, blockCount: 1}}
	// Committed parent no longer matches the witness parent header — as if the
	// witness pre-state root were forged.
	view.Carry.TransitionInput.ParentBlockHash = common.HexToHash(testHash("99"))
	if err := validateAnchorL1Linkage(view, proposal, []anchorV4CheckpointView{cp}, spans, parentAnchor); err == nil {
		t.Fatalf("bypass path must reject an unbound witness parent header")
	}
}

// TestDecodeProposalStateNodesRealFixture gives the forced-inclusion / bypass
// CheckpointStore decode real-wire coverage. The real mainnet fixture is
// normal-path, so it never invokes readParentL2Storage, but its ~5.9k
// proposal_state_nodes are genuine raiko2 output; decoding them confirms the
// bare-hex wire form holds against real data, not just synthetic fixtures.
func TestDecodeProposalStateNodesRealFixture(t *testing.T) {
	view := loadRealFixtureView(t)
	raws := view.Raw.ProposalStateNodes
	if len(raws) == 0 {
		t.Fatal("real fixture has no proposal_state_nodes")
	}
	nodes, err := decodeProposalStateNodes(raws)
	if err != nil {
		t.Fatalf("decode real proposal_state_nodes: %v", err)
	}
	if len(nodes) != len(raws) {
		t.Fatalf("decoded node count %d != input %d", len(nodes), len(raws))
	}
	for i, node := range nodes {
		if len(node) == 0 {
			t.Fatalf("proposal_state_nodes[%d] decoded to empty bytes", i)
		}
	}
}

// newCheckpointStoreStateFixture builds a GuestInputView whose first witness
// carries a coherent nested trie: an account trie (its root is the pre-state
// root) containing a CheckpointStore account whose storageRoot points at a
// storage trie holding blockHashSlot=>wantHash and stateRootSlot=>wantRoot.
//
// The account-trie nodes are placed in the witness's own state set while the
// storage-trie nodes are placed in view.Raw.ProposalStateNodes, so a passing
// read proves readParentL2Storage merges both node sources before opening the
// pre-state.
func newCheckpointStoreStateFixture(t *testing.T) (*GuestInputView, common.Address, uint64, common.Hash, common.Hash) {
	t.Helper()

	const chainID = uint64(167001)
	return newCheckpointStoreStateFixtureWithAccount(t, testCheckpointStoreAddress(chainID))
}

func newCheckpointStoreStateFixtureWithAccount(t *testing.T, account common.Address) (*GuestInputView, common.Address, uint64, common.Hash, common.Hash) {
	t.Helper()

	const chainID = uint64(167001)
	blockNumber := uint64(24862915)
	wantHash := common.HexToHash(testHash("ab"))
	wantRoot := common.HexToHash(testHash("cd"))

	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(blockNumber)

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("open checkpoint store state: %v", err)
	}
	// Give the account a nonce so it survives commit as a non-empty object.
	statedb.SetNonce(account, 1, tracing.NonceChangeUnspecified)
	statedb.SetState(account, blockHashSlot, wantHash)
	statedb.SetState(account, stateRootSlot, wantRoot)

	accountRoot, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit checkpoint store state: %v", err)
	}

	// Account-trie nodes -> witness state.
	accountTrie, err := trie.NewStateTrie(trie.StateTrieID(accountRoot), tdb)
	if err != nil {
		t.Fatalf("open account trie: %v", err)
	}
	stateAccount, err := accountTrie.GetAccount(account)
	if err != nil || stateAccount == nil {
		t.Fatalf("resolve checkpoint store account: %v", err)
	}
	witnessStateNodes := collectTrieNodes(t, accountTrie)

	// Storage-trie nodes -> proposal state nodes (shared witness resources).
	storageTrie, err := trie.NewStateTrie(
		trie.StorageTrieID(accountRoot, crypto.Keccak256Hash(account.Bytes()), stateAccount.Root),
		tdb,
	)
	if err != nil {
		t.Fatalf("open storage trie: %v", err)
	}
	storageNodes := collectTrieNodes(t, storageTrie)
	if len(storageNodes) == 0 {
		t.Fatalf("storage trie produced no nodes")
	}
	proposalStateNodes := make([]json.RawMessage, 0, len(storageNodes))
	for _, node := range storageNodes {
		proposalStateNodes = append(proposalStateNodes, mustRawMessage(t, fmt.Sprintf("%q", node)))
	}

	parentHeader := &types.Header{
		ParentHash:  common.HexToHash(testHash("01")),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress(testAddress("02")),
		Root:        accountRoot,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      new(big.Int).SetUint64(blockNumber),
		GasLimit:    30_000_000,
		GasUsed:     0,
		Time:        1_000,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
		BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
	}
	childHeader := &types.Header{
		ParentHash:  parentHeader.Hash(),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress(testAddress("02")),
		Root:        common.HexToHash(testHash("55")),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      new(big.Int).SetUint64(blockNumber + 1),
		GasLimit:    30_000_000,
		GasUsed:     0,
		Time:        1_012,
		Extra:       []byte{},
		MixDigest:   common.Hash{},
		Nonce:       types.BlockNonce{},
		BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
	}

	blockJSON := mustRawMessage(t, fmt.Sprintf(`{
		"header": %s,
		"body": {"transactions": [], "ommers": [], "withdrawals": []}
	}`, headerJSON(t, childHeader)))
	witnessStateJSON, err := json.Marshal(witnessStateNodes)
	if err != nil {
		t.Fatalf("marshal witness state: %v", err)
	}

	// Coherent short L1 chain ending at the origin block. The bypass test sets
	// proposal.OriginBlockNumber = parentAnchor + 600 (> the mainnet 512 anchor
	// offset), so origin is fixed here at blockNumber + 600. The origin checks in
	// validateAnchorL1Linkage run before the bypass/forced branch, so l1_header
	// must equal an origin header at that number; l1_ancestor_headers are
	// contiguous and parent-linked so the forced-inclusion test's normal
	// checkpoint can bind to a real ancestor.
	originBlockNumber := blockNumber + 600
	l1Headers := buildContiguousL1Headers(originBlockNumber-2, originBlockNumber)
	originHeader := l1Headers[len(l1Headers)-1]
	l1AncestorsJSON := make([]string, 0, len(l1Headers))
	for _, header := range l1Headers {
		l1AncestorsJSON = append(l1AncestorsJSON, headerJSON(t, header))
	}
	taikoJSON := fmt.Sprintf(`{
		"chain_spec": {"chain_id": %d},
		"l1_header": %s,
		"l1_ancestor_headers": [%s]
	}`, chainID, headerJSON(t, originHeader), strings.Join(l1AncestorsJSON, ","))

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {"chain_id": %d, "checkpoint_store_contract": %q},
				"witness": {"state": %s, "state_indices": [], "codes": [], "headers": [%s]},
				"accounts": {}
			}`,
				blockJSON,
				chainID,
				account.Hex(),
				witnessStateJSON,
				fmt.Sprintf(`{"header": %s, "hash": %q}`, headerJSON(t, parentHeader), parentHeader.Hash().Hex()),
			)),
		},
		Taiko:                   mustRawMessage(t, taikoJSON),
		ProposalAncestorHeaders: []json.RawMessage{},
		ProposalStateNodes:      proposalStateNodes,
	}

	view := &GuestInputView{Raw: input}
	witnessView, _, err := decodeGuestInputWitness(input.Witnesses[0])
	if err != nil {
		t.Fatalf("decode fixture witness: %v", err)
	}
	view.Witnesses = []GuestInputWitnessView{witnessView}
	view.TaikoRaw = input.Taiko
	view.GuestInputChainID = chainID
	// The witness parent header is the committed transition parent, so bind it —
	// readParentL2Storage now requires Headers[0].Hash() == ParentBlockHash before
	// trusting the pre-state root.
	view.Carry.TransitionInput.ParentBlockHash = parentHeader.Hash()

	return view, account, blockNumber, wantHash, wantRoot
}

// buildContiguousL1Headers synthesizes a contiguous, parent-linked L1 header
// chain covering [from, to] inclusive. Each header carries a distinct state root
// so a checkpoint bound to any of them (blockHash + stateRoot) is uniquely
// identified. Used by newCheckpointStoreStateFixture to give the origin and
// forced-inclusion checkpoint tests a real L1 chain to bind against.
func buildContiguousL1Headers(from, to uint64) []*types.Header {
	headers := make([]*types.Header, 0, to-from+1)
	var parentHash common.Hash
	for number := from; number <= to; number++ {
		header := &types.Header{
			ParentHash:  parentHash,
			UncleHash:   types.EmptyUncleHash,
			Coinbase:    common.HexToAddress(testAddress("0a")),
			Root:        common.HexToHash(fmt.Sprintf("0x%064x", 0x200000+number)),
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			Bloom:       types.Bloom{},
			Difficulty:  big.NewInt(0),
			Number:      new(big.Int).SetUint64(number),
			GasLimit:    30_000_000,
			GasUsed:     0,
			Time:        900_000 + number,
			Extra:       []byte{},
			MixDigest:   common.Hash{},
			Nonce:       types.BlockNonce{},
			BaseFee:     new(big.Int).SetUint64(manifestTestBaseFee),
		}
		parentHash = header.Hash()
		headers = append(headers, header)
	}
	return headers
}

// fixtureOriginHash returns the hash of the taiko.l1_header embedded in the view
// by newCheckpointStoreStateFixture. The bypass/forced-inclusion tests use it as
// proposal.OriginBlockHash so the origin checks in validateAnchorL1Linkage pass.
func fixtureOriginHash(t *testing.T, view *GuestInputView) common.Hash {
	t.Helper()
	origin, _, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil {
		t.Fatalf("decode fixture origin header: %v", err)
	}
	return origin.Hash()
}

func collectTrieNodes(t *testing.T, tr *trie.StateTrie) []string {
	t.Helper()
	it, err := tr.NodeIterator(nil)
	if err != nil {
		t.Fatalf("iterate trie: %v", err)
	}
	nodes := make([]string, 0, 4)
	for it.Next(true) {
		if it.Hash() == (common.Hash{}) {
			continue
		}
		blob := it.NodeBlob()
		if len(blob) == 0 {
			continue
		}
		nodes = append(nodes, "0x"+hex.EncodeToString(blob))
	}
	return nodes
}

func TestShastaCheckpointStorageSlots(t *testing.T) {
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(24862915)
	var buf [64]byte
	new(big.Int).SetUint64(24862915).FillBytes(buf[0:32])
	new(big.Int).SetUint64(254).FillBytes(buf[32:64])
	want := crypto.Keccak256Hash(buf[:])
	if blockHashSlot != want {
		t.Fatalf("blockHashSlot: got %s want %s", blockHashSlot.Hex(), want.Hex())
	}
	wantSR := common.BigToHash(new(big.Int).Add(want.Big(), big.NewInt(1)))
	if stateRootSlot != wantSR {
		t.Fatalf("stateRootSlot: got %s want %s", stateRootSlot.Hex(), wantSR.Hex())
	}
}
