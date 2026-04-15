package prover

import (
	"encoding/json"
	"fmt"

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
		if index > 0 {
			prev := blocks[index-1]
			if view.Number != prev.Number+1 {
				return nil, fmt.Errorf(
					"block numbers must be contiguous: got %d after %d",
					view.Number,
					prev.Number,
				)
			}
			if view.ParentHash != prev.Hash {
				return nil, fmt.Errorf(
					"block parent hash mismatch at index %d: got %s expected %s",
					index,
					view.ParentHash.Hex(),
					prev.Hash.Hex(),
				)
			}
		}
		blocks = append(blocks, view)
	}

	if first := blocks[0]; first.ParentHash != carry.TransitionInput.ParentBlockHash {
		return nil, fmt.Errorf(
			"first block parent hash mismatch: block=%s checkpoint=%s",
			first.ParentHash.Hex(),
			carry.TransitionInput.ParentBlockHash.Hex(),
		)
	}

	last := blocks[len(blocks)-1]
	if last.Number != carry.TransitionInput.Checkpoint.BlockNumber {
		return nil, fmt.Errorf(
			"checkpoint block number mismatch: block=%d checkpoint=%d",
			last.Number,
			carry.TransitionInput.Checkpoint.BlockNumber,
		)
	}
	if last.Hash != carry.TransitionInput.Checkpoint.BlockHash {
		return nil, fmt.Errorf(
			"checkpoint block hash mismatch: block=%s checkpoint=%s",
			last.Hash.Hex(),
			carry.TransitionInput.Checkpoint.BlockHash.Hex(),
		)
	}
	if last.StateRoot != carry.TransitionInput.Checkpoint.StateRoot {
		return nil, fmt.Errorf(
			"checkpoint state root mismatch: block=%s checkpoint=%s",
			last.StateRoot.Hex(),
			carry.TransitionInput.Checkpoint.StateRoot.Hex(),
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
		Hash:         header.Hash(),
		ParentHash:   header.ParentHash,
		StateRoot:    header.Root,
		ReceiptsRoot: header.ReceiptHash,
	}, nil
}

func decodeCarry(raw json.RawMessage) (CarryView, error) {
	var decoded rawProofCarryData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CarryView{}, fmt.Errorf("unmarshal proof_carry_data: %w", err)
	}

	verifier, err := parseAddressString(decoded.Verifier)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.verifier: %w", err)
	}
	proposalHash, err := parseHashString(decoded.TransitionInput.ProposalHash)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.proposal_hash: %w", err)
	}
	parentProposalHash, err := parseHashString(decoded.TransitionInput.ParentProposalHash)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.parent_proposal_hash: %w", err)
	}
	parentBlockHash, err := parseHashString(decoded.TransitionInput.ParentBlockHash)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.parent_block_hash: %w", err)
	}
	actualProver, err := parseAddressString(decoded.TransitionInput.ActualProver)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.actual_prover: %w", err)
	}
	proposer, err := parseAddressString(decoded.TransitionInput.Transition.Proposer)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.transition.proposer: %w", err)
	}
	checkpointBlockHash, err := parseHashString(decoded.TransitionInput.Checkpoint.BlockHash)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint.blockHash: %w", err)
	}
	checkpointStateRoot, err := parseHashString(decoded.TransitionInput.Checkpoint.StateRoot)
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint.stateRoot: %w", err)
	}

	return CarryView{
		ChainID:  decoded.ChainID,
		Verifier: verifier,
		TransitionInput: TransitionInputView{
			ProposalID:         decoded.TransitionInput.ProposalID,
			ProposalHash:       proposalHash,
			ParentProposalHash: parentProposalHash,
			ParentBlockHash:    parentBlockHash,
			ActualProver:       actualProver,
			Transition: TransitionView{
				Proposer:  proposer,
				Timestamp: decoded.TransitionInput.Transition.Timestamp,
			},
			Checkpoint: CheckpointView{
				BlockNumber: uint64(decoded.TransitionInput.Checkpoint.BlockNumber),
				BlockHash:   checkpointBlockHash,
				StateRoot:   checkpointStateRoot,
			},
		},
	}, nil
}
