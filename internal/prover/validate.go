package prover

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type rawProofCarryData struct {
	ChainID         uint64             `json:"chain_id"`
	Verifier        string             `json:"verifier"`
	TransitionInput rawTransitionInput `json:"transition_input"`
}

type rawTransitionInput struct {
	ProposalID         uint64        `json:"proposal_id"`
	ProposalHash       string        `json:"proposal_hash"`
	ParentProposalHash string        `json:"parent_proposal_hash"`
	ParentBlockHash    string        `json:"parent_block_hash"`
	ActualProver       string        `json:"actual_prover"`
	Transition         rawTransition `json:"transition"`
	Checkpoint         rawCheckpoint `json:"checkpoint"`
}

type rawTransition struct {
	Proposer  string `json:"proposer"`
	Timestamp uint64 `json:"timestamp"`
}

type rawCheckpoint struct {
	BlockNumber quantityValue `json:"blockNumber"`
	BlockHash   string        `json:"blockHash"`
	StateRoot   string        `json:"stateRoot"`
}

func ValidateRequest(req protocol.ShastaRequest) (*ValidatedRequest, error) {
	if req.Schema != protocol.ShastaSchemaV1 {
		return nil, fmt.Errorf("unsupported schema %q", req.Schema)
	}
	if len(req.Payload.Blocks) == 0 {
		return nil, fmt.Errorf("request must include at least one replay block")
	}

	carry, err := decodeCarry(req.Payload.ProofCarryData)
	if err != nil {
		return nil, err
	}
	if carry.ChainID != req.Payload.ChainID {
		return nil, fmt.Errorf(
			"chain_id mismatch: payload=%d proof_carry_data=%d",
			req.Payload.ChainID,
			carry.ChainID,
		)
	}

	blocks := make([]BlockView, 0, len(req.Payload.Blocks))
	for index, block := range req.Payload.Blocks {
		view, err := decodeBlock(block)
		if err != nil {
			return nil, fmt.Errorf("decode replay block %d: %w", index, err)
		}
		if index > 0 && view.Number != blocks[index-1].Number+1 {
			return nil, fmt.Errorf(
				"block numbers must be contiguous: got %d after %d",
				view.Number,
				blocks[index-1].Number,
			)
		}
		blocks = append(blocks, view)
	}

	if first := blocks[0]; !strings.EqualFold(first.ParentHash, carry.ParentBlockHash) {
		return nil, fmt.Errorf(
			"first block parent hash mismatch: block=%s checkpoint=%s",
			first.ParentHash,
			carry.ParentBlockHash,
		)
	}

	last := blocks[len(blocks)-1]
	if last.Number != carry.Checkpoint.BlockNumber {
		return nil, fmt.Errorf(
			"checkpoint block number mismatch: block=%d checkpoint=%d",
			last.Number,
			carry.Checkpoint.BlockNumber,
		)
	}
	if !strings.EqualFold(last.StateRoot, carry.Checkpoint.StateRoot) {
		return nil, fmt.Errorf(
			"checkpoint state root mismatch: block=%s checkpoint=%s",
			last.StateRoot,
			carry.Checkpoint.StateRoot,
		)
	}

	return &ValidatedRequest{
		Request: req,
		Carry:   carry,
		Blocks:  blocks,
	}, nil
}

func decodeBlock(block protocol.ReplayBlock) (BlockView, error) {
	decoded, err := decodeBlockEnvelope(block.Block)
	if err != nil {
		return BlockView{}, err
	}
	header, err := decodeHeader(decoded.Header)
	if err != nil {
		return BlockView{}, err
	}

	return BlockView{
		Number:       header.Number.Uint64(),
		ParentHash:   header.ParentHash.Hex(),
		StateRoot:    header.Root.Hex(),
		ReceiptsRoot: header.ReceiptHash.Hex(),
	}, nil
}

func decodeCarry(raw json.RawMessage) (CarryView, error) {
	var decoded rawProofCarryData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CarryView{}, fmt.Errorf("unmarshal proof_carry_data: %w", err)
	}

	return CarryView{
		ChainID:         decoded.ChainID,
		ParentBlockHash: decoded.TransitionInput.ParentBlockHash,
		Checkpoint: CheckpointView{
			BlockNumber: uint64(decoded.TransitionInput.Checkpoint.BlockNumber),
			BlockHash:   decoded.TransitionInput.Checkpoint.BlockHash,
			StateRoot:   decoded.TransitionInput.Checkpoint.StateRoot,
		},
	}, nil
}
