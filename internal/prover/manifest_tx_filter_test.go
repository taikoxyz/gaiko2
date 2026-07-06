package prover

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
	result, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	filtered := result.transactions
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

	result, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	filtered := result.transactions
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

	result, err := filterManifestTransactions(
		context.Background(),
		fixture.chainID,
		block,
		manifestTxs,
		witness,
	)
	if err != nil {
		t.Fatalf("filter manifest transactions: %v", err)
	}
	filtered := result.transactions
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

func TestManifestCandidateStateErrorDefersNonAnchorZkGasTruncation(t *testing.T) {
	stateErr := errors.New("missing trie node")

	fatalErr, deferredErr := manifestCandidateStateError(stateErr, vm.ErrZkGasLimitExceeded, true, 1)
	if fatalErr != nil {
		t.Fatalf("expected non-anchor zk-gas truncation to defer state error, got fatal %v", fatalErr)
	}
	if !errors.Is(deferredErr, stateErr) {
		t.Fatalf("expected non-anchor zk-gas truncation to preserve deferred state error, got %v", deferredErr)
	}

	fatalErr, deferredErr = manifestCandidateStateError(stateErr, vm.ErrZkGasLimitExceeded, true, 0)
	if !errors.Is(fatalErr, stateErr) {
		t.Fatalf("expected anchor state error to remain fatal, got %v", fatalErr)
	}
	if deferredErr != nil {
		t.Fatalf("expected anchor state error not to be deferred, got %v", deferredErr)
	}

	fatalErr, deferredErr = manifestCandidateStateError(stateErr, errors.New("other apply error"), true, 1)
	if !errors.Is(fatalErr, stateErr) {
		t.Fatalf("expected non-zk-gas state error to remain fatal, got %v", fatalErr)
	}
	if deferredErr != nil {
		t.Fatalf("expected non-zk-gas state error not to be deferred, got %v", deferredErr)
	}

	fatalErr, deferredErr = manifestCandidateStateError(stateErr, vm.ErrZkGasLimitExceeded, false, 1)
	if !errors.Is(fatalErr, stateErr) {
		t.Fatalf("expected pre-Unzen state error to remain fatal, got %v", fatalErr)
	}
	if deferredErr != nil {
		t.Fatalf("expected pre-Unzen state error not to be deferred, got %v", deferredErr)
	}
}

func TestManifestTransactionRootMismatchPreservesDeferredStateError(t *testing.T) {
	stateErr := errors.New("missing trie node")
	err := manifestTransactionRootMismatchError(
		common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		stateErr,
	)

	if !errors.Is(err, stateErr) {
		t.Fatalf("expected transaction root mismatch to preserve deferred state error, got %v", err)
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
