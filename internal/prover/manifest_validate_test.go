package prover

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

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
			name: "invalid blob encoding",
			mutate: func(f *manifestBindingFixture) {
				f.blobBacked = true
				f.corruptBlobEncoding = true
			},
			wantErr: "invalid blob encoding",
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
	anchorBlockNumber           uint64
	omitAnchorTx                bool
	omitUserTx                  bool
	addManifestBlock            bool
	witnessStateNodes           []string
	witnessCodes                []string
	omitProposalAncestorHeaders bool
	omitGrandparentHeader       bool
	blobBacked                  bool
	corruptBlobEncoding         bool
	isForcedInclusion           bool
}

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
		manifestTimestamp:  1_001,
		manifestCoinbase:   common.HexToAddress(testAddress("22")),
		manifestGasLimit:   manifestGasLimit,
		manifestUserTxJSON: manifestTx,
		blockUserTxJSON:    manifestTx,
		blockTimestamp:     1_001,
		blockCoinbase:      common.HexToAddress(testAddress("22")),
		blockNumber:        blockNumber,
		blockGasLimit:      manifestGasLimit + 1_000_000,
		blockExtra:         manifestExtraData(42, proposalID),
		blockMixDigest:     manifestMixHash(common.BigToHash(parentDifficulty), blockNumber),
		blockBaseFee:       manifestTestBaseFee,
		l2Contract:         common.HexToAddress(testAddress("44")),
		anchorTo:           common.HexToAddress(testAddress("44")),
		anchorBlockNumber:  900,
		witnessStateNodes:  witnessStateNodes,
	}
	fixture.parentHeader.ParentHash = fixture.grandparentHeader.Hash()
	return fixture
}

func (f *manifestBindingFixture) view(t *testing.T) *GuestInputView {
	t.Helper()

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
				"actual_prover": %q,
				"last_anchor_block_number": 899
			},
			"data_sources": [%s]
		}`, f.chainID, f.proposalID, proposalJSON, testAddress("77"), dataSourceJSON)),
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
	blocks := []testManifestBlock{{
		Timestamp:         f.manifestTimestamp,
		Coinbase:          f.manifestCoinbase,
		AnchorBlockNumber: 900,
		GasLimit:          f.manifestGasLimit,
		Transactions:      userTxs,
	}}
	if f.addManifestBlock {
		blocks = append(blocks, testManifestBlock{
			Timestamp:         f.manifestTimestamp + 1,
			Coinbase:          f.manifestCoinbase,
			AnchorBlockNumber: 901,
			GasLimit:          f.manifestGasLimit,
			Transactions:      types.Transactions{},
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
	input := anchorInput(t, f.anchorBlockNumber, common.HexToHash(testHash("61")), common.HexToHash(testHash("62")))
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x1", "s": "0x1", "yParity": "0x0"},
		"transaction": {
			"Eip1559": {
				"chain_id": "0x%x",
				"nonce": "0x0",
				"max_priority_fee_per_gas": "0x0",
				"max_fee_per_gas": "0x%x",
				"gas": "0xf4240",
				"to": %q,
				"value": "0x0",
				"input": %q,
				"access_list": []
			}
		}
	}`, f.chainID, f.blockBaseFee, f.anchorTo.Hex(), "0x"+hex.EncodeToString(input)))
}

func (f *manifestBindingFixture) chainSpecJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"l2_contract": %q,
		"hard_forks": {"SHASTA": {"Block": 0}},
		"verifier_address_forks": {"SHASTA": {"SgxGeth": %q}}
	}`, f.chainID, f.l2Contract.Hex(), testAddress("f9")))
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
		testHash("cd"),
		sourceJSON,
	))
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
