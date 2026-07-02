package prover

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
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

func mustChainConfig(t *testing.T, chainID uint64) *params.ChainConfig {
	t.Helper()
	config, err := chainConfigFor(chainID)
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}
	return config
}

func mustWitnessStateDB(t *testing.T, witness *ReplayWitness) *state.StateDB {
	t.Helper()
	memdb := witness.Witness.MakeHashDB()
	statedb, err := state.New(
		witness.Witness.Root(),
		state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)),
	)
	if err != nil {
		t.Fatalf("open witness state: %v", err)
	}
	return statedb
}
