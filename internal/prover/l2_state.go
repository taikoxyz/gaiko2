package prover

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/triedb"
)

func readParentL2Storage(view *GuestInputView, account common.Address, slot common.Hash) (common.Hash, error) {
	if len(view.Witnesses) == 0 {
		return common.Hash{}, fmt.Errorf("guest input must include at least one witness")
	}
	_, replayWitness, err := decodeReplayBlock(view.Witnesses[0].ReplayBlock)
	if err != nil {
		return common.Hash{}, fmt.Errorf("decode first witness block: %w", err)
	}
	if len(replayWitness.Witness.Headers) == 0 || replayWitness.Witness.Headers[0] == nil {
		return common.Hash{}, fmt.Errorf("first witness missing parent header")
	}
	preStateRoot := replayWitness.Witness.Headers[0].Root

	witness := &stateless.Witness{
		Headers: replayWitness.Witness.Headers,
		Codes:   replayWitness.Witness.Codes,
		State:   make(map[string]struct{}, len(replayWitness.Witness.State)+len(view.Raw.ProposalStateNodes)),
	}
	for node := range replayWitness.Witness.State {
		witness.State[node] = struct{}{}
	}
	for i, raw := range view.Raw.ProposalStateNodes {
		var hexNode string
		if err := json.Unmarshal(raw, &hexNode); err != nil {
			return common.Hash{}, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		decoded, err := hexutil.Decode(hexNode)
		if err != nil {
			return common.Hash{}, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		witness.State[string(decoded)] = struct{}{}
	}

	memdb := witness.MakeHashDB()
	db, err := state.New(preStateRoot,
		state.NewDatabase(triedb.NewDatabase(memdb, triedb.HashDefaults), state.NewCodeDB(memdb)))
	if err != nil {
		return common.Hash{}, fmt.Errorf("open parent state at %s: %w", preStateRoot.Hex(), err)
	}
	// GetState swallows trie-expansion failures (e.g. a missing witness node)
	// into the StateDB's deferred error, returning a zero slot. Surface that
	// error explicitly so an incomplete or corrupt witness cannot masquerade as
	// a legitimately empty storage slot.
	value := db.GetState(account, slot)
	if err := db.Error(); err != nil {
		return common.Hash{}, fmt.Errorf("read %s slot %s from parent state at %s: %w",
			account.Hex(), slot.Hex(), preStateRoot.Hex(), err)
	}
	return value, nil
}
