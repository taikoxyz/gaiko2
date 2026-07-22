package prover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

func TestFilterManifestTransactionsMatchesCanonicalBlock(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)

	block, witness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}

	manifestTxs := types.Transactions{decodeTestTransaction(t, fixture.manifestUserTxJSON)}
	filtered, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	if len(filtered) != len(block.Transactions()) {
		t.Fatalf("filtered count mismatch: got %d want %d", len(filtered), len(block.Transactions()))
	}
	for index, tx := range block.Transactions() {
		if filtered[index].Hash() != tx.Hash() {
			t.Fatalf("filtered tx %d hash mismatch: got %s want %s", index, filtered[index].Hash(), tx.Hash())
		}
	}
}

func TestFilterManifestTransactionsRevertsInsufficientBalanceCandidate(t *testing.T) {
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

	block, witness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	manifestTxs := decodeTestTransactions(t, fixture.manifestUserTxJSONs)

	filtered, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	assertFilteredMatchesCanonicalBlock(t, filtered, block.Transactions())
}

func TestFilterManifestTransactionsStopsAtUnzenZkGasLimit(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	manifestTxJSONs := manifestSequentialUserTxJSONs(t, fixture.chainID, 450)
	fixture.manifestUserTxJSONs = manifestTxJSONs
	fixture.blockUserTxJSONs = manifestTxJSONs

	signer := manifestTestTxSigner(t)
	witnessStateNodes, witnessStateRoot := witnessStateNodesWithBalance(t, signer, new(big.Int).SetUint64(1_000_000_000_000_000_000))
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes
	view := fixture.view(t)

	block, witness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	manifestTxs := decodeTestTransactions(t, manifestTxJSONs)

	filtered, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	if len(filtered) <= 1 {
		t.Fatalf("expected at least anchor plus one committed transaction, got %d", len(filtered))
	}
	if len(filtered) >= len(manifestTxs)+1 {
		t.Fatalf("expected zkGas filtering to truncate manifest txs, got %d committed for %d manifest txs", len(filtered), len(manifestTxs))
	}
	if filtered[0].Hash() != block.Transactions()[0].Hash() {
		t.Fatalf("anchor hash mismatch: got %s want %s", filtered[0].Hash(), block.Transactions()[0].Hash())
	}
	for index := 1; index < len(filtered); index++ {
		if filtered[index].Hash() != manifestTxs[index-1].Hash() {
			t.Fatalf("filtered tx %d hash mismatch: got %s want %s", index, filtered[index].Hash(), manifestTxs[index-1].Hash())
		}
	}
}

func TestFilterManifestTransactionsTruncatesZkGasBeforeWitnessError(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	loopContract := common.HexToAddress(testAddress("46"))
	loopCode := runtimeZkGasAfterMissingStorageCode()
	witnessStateNodes, witnessCodes, witnessStateRoot := witnessStateNodesWithMissingStorageAndCode(
		t,
		manifestTestTxSigner(t),
		new(big.Int).SetUint64(1_000_000_000_000_000_000),
		loopContract,
		loopCode,
	)
	fixture.parentHeader.Root = witnessStateRoot
	fixture.witnessStateNodes = witnessStateNodes
	fixture.witnessCodes = witnessCodes

	const fillerCount = 400
	manifestTxJSONs := manifestSequentialUserTxJSONs(t, fixture.chainID, fillerCount)
	manifestTxJSONs = append(
		manifestTxJSONs,
		manifestUserTxJSONWithGas(t, fixture.chainID, fillerCount, loopContract.Hex(), 5_000_000),
	)
	fixture.manifestUserTxJSONs = manifestTxJSONs
	fixture.blockUserTxJSONs = []json.RawMessage{}
	view := fixture.view(t)

	block, witness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	filtered, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		decodeTestTransactions(t, manifestTxJSONs),
		witness,
	)
	if err != nil {
		t.Fatalf("expected zk-gas truncation to ignore deferred witness error, got %v", err)
	}
	if len(filtered) <= 1 {
		t.Fatalf("expected anchor and committed filler transactions, got %d", len(filtered))
	}
	if len(filtered) >= len(manifestTxJSONs)+1 {
		t.Fatalf("expected zk-gas truncation, committed %d of %d candidates", len(filtered), len(manifestTxJSONs)+1)
	}
}

func TestFilterManifestTransactionsHonorsCanceledContext(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	view := fixture.view(t)

	block, witness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = filterManifestTransactions(
		ctx,
		fixture.chainID,
		block,
		types.Transactions{decodeTestTransaction(t, fixture.manifestUserTxJSON)},
		witness,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestIsRecoverableNonAnchorTxError(t *testing.T) {
	recoverable := []error{
		vm.ErrZkGasLimitExceeded,
		core.ErrGasLimitReached,
		core.ErrGasLimitOverflow,
		core.ErrNonceTooLow,
		core.ErrNonceTooHigh,
		core.ErrNonceMax,
		core.ErrInsufficientFunds,
		core.ErrInsufficientFundsForTransfer,
		core.ErrInsufficientBalanceWitness,
		core.ErrGasUintOverflow,
		core.ErrIntrinsicGas,
		core.ErrFloorDataGas,
		core.ErrTxTypeNotSupported,
		core.ErrTipAboveFeeCap,
		core.ErrTipVeryHigh,
		core.ErrFeeCapVeryHigh,
		core.ErrFeeCapTooLow,
		core.ErrSenderNoEOA,
		core.ErrBlobFeeCapTooLow,
		core.ErrMissingBlobHashes,
		core.ErrTooManyBlobs,
		core.ErrBlobTxCreate,
		core.ErrEmptyAuthList,
		core.ErrSetCodeTxCreate,
		core.ErrGasLimitTooHigh,
		vm.ErrMaxInitCodeSizeExceeded,
	}
	for _, err := range recoverable {
		if !isRecoverableNonAnchorTxError(err) {
			t.Fatalf("expected %v to be recoverable", err)
		}
		if !isRecoverableNonAnchorTxError(fmt.Errorf("wrapped: %w", err)) {
			t.Fatalf("expected wrapped %v to be recoverable", err)
		}
	}

	fatal := errors.New("fatal execution error")
	if isRecoverableNonAnchorTxError(fatal) {
		t.Fatal("unrelated error must be fatal")
	}
	if isRecoverableNonAnchorTxError(fmt.Errorf("wrapped: %w", fatal)) {
		t.Fatal("wrapped unrelated error must be fatal")
	}
}

func assertFilteredMatchesCanonicalBlock(t *testing.T, filtered types.Transactions, canonical types.Transactions) {
	t.Helper()
	if len(filtered) != len(canonical) {
		t.Fatalf("filtered count mismatch: got %d want %d", len(filtered), len(canonical))
	}
	for index, tx := range canonical {
		if filtered[index].Hash() != tx.Hash() {
			t.Fatalf("filtered tx %d hash mismatch: got %s want %s", index, filtered[index].Hash(), tx.Hash())
		}
	}
}

func decodeTestTransactions(t *testing.T, raws []json.RawMessage) types.Transactions {
	t.Helper()
	txs := make(types.Transactions, 0, len(raws))
	for _, raw := range raws {
		txs = append(txs, decodeTestTransaction(t, raw))
	}
	return txs
}

func manifestSequentialUserTxJSONs(t *testing.T, chainID uint64, count int) []json.RawMessage {
	t.Helper()
	raws := make([]json.RawMessage, 0, count)
	for nonce := 0; nonce < count; nonce++ {
		raws = append(raws, manifestUserTxJSON(t, chainID, uint64(nonce), testAddress("33")))
	}
	return raws
}
