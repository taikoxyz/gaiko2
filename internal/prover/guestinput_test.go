package prover

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestDecodeGuestInputExtractsReplayCompatibleViews(t *testing.T) {
	parentHash := testHash("11")
	stateRoot := testHash("22")
	receiptsRoot := testHash("33")
	blockRaw := sampleReplayBlock(t, "0x2a", parentHash, stateRoot, receiptsRoot)
	blockHash := replayBlockHash(t, blockRaw)

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {"chainId": 167013},
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {"0x0000000000000000000000000000000000000000": {"balance": "0x0"}}
			}`, blockRaw)),
		},
		Taiko: mustRawMessage(t, `{
			"proposal_id": 77,
			"proposal_event": {
				"proposal": {
					"id": 88
				}
			},
			"data_sources": [
				{"source": "calldata"},
				{"source": "blob"}
			]
		}`),
		ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot),
	}

	view, err := DecodeGuestInput(input)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}

	if view.ChainID != 167013 {
		t.Fatalf("unexpected chain id: %d", view.ChainID)
	}
	if len(view.Witnesses) != 1 {
		t.Fatalf("unexpected witness count: %d", len(view.Witnesses))
	}
	if len(view.Blocks) != 1 {
		t.Fatalf("unexpected block count: %d", len(view.Blocks))
	}

	block := view.Blocks[0]
	if block.Number != 42 {
		t.Fatalf("unexpected block number: %d", block.Number)
	}
	if block.Hash != common.HexToHash(blockHash) {
		t.Fatalf("unexpected block hash: %s", block.Hash)
	}
	if block.ParentHash != common.HexToHash(parentHash) {
		t.Fatalf("unexpected parent hash: %s", block.ParentHash)
	}
	if block.StateRoot != common.HexToHash(stateRoot) {
		t.Fatalf("unexpected state root: %s", block.StateRoot)
	}

	if view.Taiko.ProposalID != 77 {
		t.Fatalf("unexpected taiko proposal id: %d", view.Taiko.ProposalID)
	}
	if view.Taiko.ProposalEventProposalID != 88 {
		t.Fatalf("unexpected proposal event proposal id: %d", view.Taiko.ProposalEventProposalID)
	}
	if view.Taiko.DataSourceCount != 2 {
		t.Fatalf("unexpected data source count: %d", view.Taiko.DataSourceCount)
	}
	if view.Carry.TransitionInput.Checkpoint.BlockNumber != 42 {
		t.Fatalf("unexpected checkpoint block number: %d", view.Carry.TransitionInput.Checkpoint.BlockNumber)
	}
	if view.Carry.TransitionInput.Checkpoint.BlockHash != common.HexToHash(blockHash) {
		t.Fatalf("unexpected checkpoint block hash: %s", view.Carry.TransitionInput.Checkpoint.BlockHash)
	}
	if view.Carry.TransitionInput.Checkpoint.StateRoot != common.HexToHash(stateRoot) {
		t.Fatalf("unexpected checkpoint state root: %s", view.Carry.TransitionInput.Checkpoint.StateRoot)
	}

	witness := view.Witnesses[0]
	if string(witness.Raw) == "" {
		t.Fatal("expected raw witness to be retained")
	}
	if string(witness.BlockRaw) == "" {
		t.Fatal("expected raw witness block to be retained")
	}
	if string(witness.ChainSpecRaw) == "" {
		t.Fatal("expected raw witness chain spec to be retained")
	}
	if string(witness.WitnessRaw) == "" {
		t.Fatal("expected raw witness payload to be retained")
	}
	if string(witness.AccountsRaw) == "" {
		t.Fatal("expected raw witness accounts to be retained")
	}
}

func TestDecodeGuestInputRejectsEmptyWitnessList(t *testing.T) {
	_, err := DecodeGuestInput(protocol.ShastaGuestInput{
		ProofCarryData: sampleCarryData(t, 167013, testHash("11"), "0x2a", testHash("22"), testHash("33")),
	})
	if err == nil || err.Error() != "guest input must include at least one witness" {
		t.Fatalf("unexpected error: %v", err)
	}
}
