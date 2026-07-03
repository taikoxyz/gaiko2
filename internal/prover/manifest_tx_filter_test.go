package prover

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
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
