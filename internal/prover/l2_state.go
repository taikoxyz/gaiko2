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
	parentHeader := replayWitness.Witness.Headers[0]
	// The witness parent header's state root is only a trustworthy pre-state root
	// if that header is the committed transition parent. Bind it to
	// TransitionInput.ParentBlockHash here so this read — and therefore manifest
	// binding — is sound on its own, rather than relying on the later replay-phase
	// checks in validateReplayWitness / validateBlockViews (which run only inside
	// ReplayService.Prove).
	if got := parentHeader.Hash(); got != view.Carry.TransitionInput.ParentBlockHash {
		return common.Hash{}, fmt.Errorf(
			"witness parent header hash %s does not match committed parent block hash %s",
			got.Hex(), view.Carry.TransitionInput.ParentBlockHash.Hex())
	}
	preStateRoot := parentHeader.Root

	witness := &stateless.Witness{
		Headers: replayWitness.Witness.Headers,
		Codes:   replayWitness.Witness.Codes,
		State:   make(map[string]struct{}, len(replayWitness.Witness.State)+len(view.Raw.ProposalStateNodes)),
	}
	for node := range replayWitness.Witness.State {
		witness.State[node] = struct{}{}
	}
	proposalNodes, err := decodeProposalStateNodes(view.Raw.ProposalStateNodes)
	if err != nil {
		return common.Hash{}, err
	}
	for _, node := range proposalNodes {
		witness.State[string(node)] = struct{}{}
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

// decodeProposalStateNodes decodes the shared proposal_state_nodes pool into raw
// RLP trie-node bytes.
//
// Each entry is a bare hex string of the node's RLP bytes. This matches raiko2's
// wire form: its WitnessStateNode Serialize emits `self.bytes` (a "0x…" hex
// string) in JSON, identical to the per-witness `state` nodes. The `{hash,bytes}`
// object form in raiko2's deserializer is backcompat that it never emits, so a
// non-string entry is genuinely malformed and fails closed here. (Confirmed
// against the real mainnet fixture, whose ~5.9k proposal_state_nodes are all bare
// hex strings.)
func decodeProposalStateNodes(raws []json.RawMessage) ([][]byte, error) {
	nodes := make([][]byte, 0, len(raws))
	for i, raw := range raws {
		var hexNode string
		if err := json.Unmarshal(raw, &hexNode); err != nil {
			return nil, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		decoded, err := hexutil.Decode(hexNode)
		if err != nil {
			return nil, fmt.Errorf("proposal_state_nodes[%d]: %w", i, err)
		}
		nodes = append(nodes, decoded)
	}
	return nodes, nil
}
