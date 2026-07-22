package prover

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

// TestReplayStateDBSurfacesMissingWitnessRead proves the exact hazard closed by
// the db.Error() guards in GethRunner.Execute: reading a storage slot whose trie
// node is absent from the witness silently returns a zero value and records the
// failure only in the StateDB's deferred error. go-ethereum's stateless
// ValidateState returns before it would surface that error and IntermediateRoot
// only hashes written state, so without the explicit db.Error() check a forged
// "empty" read would flow into a self-consistent post-state that Prove accepts.
//
// The StateDB here is constructed exactly as GethRunner.Execute constructs it
// (witness.MakeHashDB -> state.New at the parent root), so the assertion that
// db.Error() is non-nil is the assertion the production guard now relies on.
func TestReplayStateDBSurfacesMissingWitnessRead(t *testing.T) {
	account := common.HexToAddress(testAddress("34"))
	slot := common.HexToHash(testHash("07"))
	value := common.HexToHash(testHash("ab"))

	memdb := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memdb, triedb.HashDefaults)
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(tdb, state.NewCodeDB(memdb)))
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	// A non-zero nonce keeps the account object non-empty through commit; the
	// slot value is non-zero so a correct read is distinguishable from the
	// zero value a missing-node read returns.
	statedb.SetNonce(account, 1, tracing.NonceChangeUnspecified)
	statedb.SetState(account, slot, value)
	root, err := statedb.Commit(0, false, false)
	if err != nil {
		t.Fatalf("commit state: %v", err)
	}

	// Carry the account-trie nodes in the witness but deliberately omit the
	// storage-trie node, so the account resolves but the slot read cannot.
	accountTrie, err := trie.NewStateTrie(trie.StateTrieID(root), tdb)
	if err != nil {
		t.Fatalf("open account trie: %v", err)
	}
	accountNodes := collectTrieNodes(t, accountTrie)
	if len(accountNodes) == 0 {
		t.Fatal("account trie produced no nodes")
	}
	witnessState := make(map[string]struct{}, len(accountNodes))
	for _, node := range accountNodes {
		witnessState[string(common.FromHex(node))] = struct{}{}
	}

	parentHeader := &types.Header{
		ParentHash:  common.HexToHash(testHash("01")),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress(testAddress("02")),
		Root:        root,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  common.Big0,
		Number:      common.Big1,
		GasLimit:    30_000_000,
		Time:        1_000,
		Extra:       []byte{},
		BaseFee:     common.Big1,
	}
	witness := &stateless.Witness{
		Headers: []*types.Header{parentHeader},
		Codes:   map[string]struct{}{},
		State:   witnessState,
	}

	nodedb := witness.MakeHashDB()
	db, err := state.New(
		witness.Root(),
		state.NewDatabase(triedb.NewDatabase(nodedb, triedb.HashDefaults), state.NewCodeDB(nodedb)),
	)
	if err != nil {
		t.Fatalf("open replay state: %v", err)
	}

	got := db.GetState(account, slot)
	if got != (common.Hash{}) {
		t.Fatalf("expected missing-node read to silently return zero, got %s", got.Hex())
	}
	if err := db.Error(); err == nil {
		t.Fatal("expected deferred witness state error after missing-node read, got nil")
	}
}
