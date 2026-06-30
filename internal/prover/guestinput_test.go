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
	chainSpecRaw := mustRawMessage(t, `{"chainId": 167013}`)
	witnessRaw := mustRawMessage(t, `{"state": [], "state_indices": [], "codes": [], "headers": []}`)
	accountsRaw := mustRawMessage(t, `{"0x0000000000000000000000000000000000000000": {"balance": "0x0"}}`)

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": %s,
				"witness": %s,
				"accounts": %s
			}`, blockRaw, chainSpecRaw, witnessRaw, accountsRaw)),
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

	if view.GuestInputChainID != 167013 {
		t.Fatalf("unexpected guest input chain id: %d", view.GuestInputChainID)
	}
	if view.CarryChainID != 167013 {
		t.Fatalf("unexpected carry chain id: %d", view.CarryChainID)
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
	if string(witness.BlockRaw) != string(blockRaw) {
		t.Fatalf("unexpected raw witness block:\n%s", witness.BlockRaw)
	}
	if string(witness.ChainSpecRaw) != string(chainSpecRaw) {
		t.Fatalf("unexpected raw witness chain spec: %s", witness.ChainSpecRaw)
	}
	if string(witness.WitnessRaw) != string(witnessRaw) {
		t.Fatalf("unexpected raw witness payload: %s", witness.WitnessRaw)
	}
	if string(witness.AccountsRaw) != string(accountsRaw) {
		t.Fatalf("unexpected raw witness accounts: %s", witness.AccountsRaw)
	}
}

func TestDecodeGuestInputExposesMismatchedChainIDsWithoutValidation(t *testing.T) {
	parentHash := testHash("11")
	stateRoot := testHash("22")
	blockRaw := sampleReplayBlock(t, "0x2a", parentHash, stateRoot, testHash("33"))
	blockHash := replayBlockHash(t, blockRaw)

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {"chain_id": 31337},
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {}
			}`, blockRaw)),
		},
		Taiko: mustRawMessage(t, `{
			"proposal_id": 77,
			"proposal_event": {"proposal": {"id": 88}},
			"data_sources": []
		}`),
		ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot),
	}

	view, err := DecodeGuestInput(input)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}

	if view.GuestInputChainID != 31337 {
		t.Fatalf("unexpected guest input chain id: %d", view.GuestInputChainID)
	}
	if view.CarryChainID != 167013 {
		t.Fatalf("unexpected carry chain id: %d", view.CarryChainID)
	}
}

func TestDecodeGuestInputFallsBackToTaikoChainID(t *testing.T) {
	parentHash := testHash("11")
	stateRoot := testHash("22")
	blockRaw := sampleReplayBlock(t, "0x2a", parentHash, stateRoot, testHash("33"))
	blockHash := replayBlockHash(t, blockRaw)

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {},
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {}
			}`, blockRaw)),
		},
		Taiko: mustRawMessage(t, `{
			"chain_spec": {"chainId": 31337},
			"proposal_id": 77,
			"proposal_event": {"proposal": {"id": 88}},
			"data_sources": []
		}`),
		ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot),
	}

	view, err := DecodeGuestInput(input)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}

	if view.GuestInputChainID != 31337 {
		t.Fatalf("unexpected guest input chain id: %d", view.GuestInputChainID)
	}
	if view.CarryChainID != 167013 {
		t.Fatalf("unexpected carry chain id: %d", view.CarryChainID)
	}
}

func TestDecodeGuestInputRejectsNullWitnessSubtrees(t *testing.T) {
	parentHash := testHash("11")
	stateRoot := testHash("22")
	blockRaw := sampleReplayBlock(t, "0x2a", parentHash, stateRoot, testHash("33"))
	blockHash := replayBlockHash(t, blockRaw)

	cases := []struct {
		name    string
		witness json.RawMessage
		wantErr string
	}{
		{
			name: "block",
			witness: mustRawMessage(t, `{
				"block": null,
				"chain_spec": {"chainId": 167013},
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {}
			}`),
			wantErr: "decode guest input witness 0: missing or null witness.block",
		},
		{
			name: "chain spec",
			witness: mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": null,
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {}
			}`, blockRaw)),
			wantErr: "decode guest input witness 0: missing or null witness.chain_spec",
		},
		{
			name: "witness",
			witness: mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {"chainId": 167013},
				"witness": null,
				"accounts": {}
			}`, blockRaw)),
			wantErr: "decode guest input witness 0: missing or null witness.witness",
		},
		{
			name: "accounts",
			witness: mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": {"chainId": 167013},
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": null
			}`, blockRaw)),
			wantErr: "decode guest input witness 0: missing or null witness.accounts",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeGuestInput(protocol.ShastaGuestInput{
				Witnesses: []json.RawMessage{tc.witness},
				Taiko: mustRawMessage(t, `{
					"proposal_id": 77,
					"proposal_event": {"proposal": {"id": 88}},
					"data_sources": []
				}`),
				ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot),
			})
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecodeGuestInputRejectsNullOrEmptyTaiko(t *testing.T) {
	parentHash := testHash("11")
	stateRoot := testHash("22")
	blockRaw := sampleReplayBlock(t, "0x2a", parentHash, stateRoot, testHash("33"))
	blockHash := replayBlockHash(t, blockRaw)

	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "empty"},
		{name: "null", raw: mustRawMessage(t, `null`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeGuestInput(protocol.ShastaGuestInput{
				Witnesses: []json.RawMessage{
					mustRawMessage(t, fmt.Sprintf(`{
						"block": %s,
						"chain_spec": {"chainId": 167013},
						"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
						"accounts": {}
					}`, blockRaw)),
				},
				Taiko:          tc.raw,
				ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot),
			})
			if err == nil || err.Error() != "missing or null taiko" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
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
