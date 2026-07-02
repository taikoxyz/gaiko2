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
	switch req.Schema {
	case protocol.ShastaRequestSchemaV1:
		return validateGuestInputRequest(req)
	default:
		return nil, validationError(fmt.Errorf("unsupported schema %q", req.Schema), requestLogMetadata(req))
	}
}

func validateGuestInputRequest(req protocol.ShastaRequest) (*ValidatedRequest, error) {
	metadata := requestLogMetadata(req)
	if req.Payload.GuestInput == nil {
		return nil, validationError(fmt.Errorf("request must include guest_input"), metadata)
	}

	view, err := DecodeGuestInput(*req.Payload.GuestInput)
	if err != nil {
		return nil, validationError(err, metadata)
	}
	metadata = requestLogMetadataFromView(req, view)
	if err := ValidateGuestInputCarry(view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := ValidateGuestInputBlobSources(view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := validateBlockViews(view.Blocks, view.Carry); err != nil {
		return nil, validationError(err, metadata)
	}

	blocks := make([]protocol.ReplayBlock, len(view.Witnesses))
	for index, witness := range view.Witnesses {
		blocks[index] = witness.ReplayBlock
	}
	normalized := protocol.ShastaRequest{
		Schema: req.Schema,
		Payload: protocol.ShastaPayload{
			ChainID:        view.GuestInputChainID,
			Blocks:         blocks,
			ProofCarryData: view.Raw.ProofCarryData,
			GuestInput:     req.Payload.GuestInput,
		},
	}

	return &ValidatedRequest{
		Request:     normalized,
		Carry:       view.Carry,
		Blocks:      view.Blocks,
		LogMetadata: metadata,
	}, nil
}

func requestLogMetadata(req protocol.ShastaRequest) RequestLogMetadata {
	return RequestLogMetadata{
		Schema:     req.Schema,
		ChainID:    req.Payload.ChainID,
		BlockCount: len(req.Payload.Blocks),
	}
}

func requestLogMetadataFromView(req protocol.ShastaRequest, view *GuestInputView) RequestLogMetadata {
	metadata := requestLogMetadata(req)
	if view.GuestInputChainID != 0 {
		metadata.ChainID = view.GuestInputChainID
	}
	metadata.BlockCount = len(view.Witnesses)
	return metadata
}

func validationError(err error, metadata RequestLogMetadata) error {
	if err == nil {
		return nil
	}
	return &ValidationError{Err: err, Metadata: metadata}
}

func validateBlockViews(blocks []BlockView, carry CarryView) error {
	for index := 1; index < len(blocks); index++ {
		prev := blocks[index-1]
		current := blocks[index]
		if current.Number != prev.Number+1 {
			return fmt.Errorf(
				"block numbers must be contiguous: got %d after %d",
				current.Number,
				prev.Number,
			)
		}
		if current.ParentHash != prev.Hash {
			return fmt.Errorf(
				"block parent hash mismatch at index %d: got %s expected %s",
				index,
				current.ParentHash.Hex(),
				prev.Hash.Hex(),
			)
		}
	}

	if first := blocks[0]; first.ParentHash != carry.TransitionInput.ParentBlockHash {
		return fmt.Errorf(
			"first block parent hash mismatch: block=%s checkpoint=%s",
			first.ParentHash.Hex(),
			carry.TransitionInput.ParentBlockHash.Hex(),
		)
	}

	last := blocks[len(blocks)-1]
	if last.Number != carry.TransitionInput.Checkpoint.BlockNumber {
		return fmt.Errorf(
			"checkpoint block number mismatch: block=%d checkpoint=%d",
			last.Number,
			carry.TransitionInput.Checkpoint.BlockNumber,
		)
	}
	if last.Hash != carry.TransitionInput.Checkpoint.BlockHash {
		return fmt.Errorf(
			"checkpoint block hash mismatch: block=%s checkpoint=%s",
			last.Hash.Hex(),
			carry.TransitionInput.Checkpoint.BlockHash.Hex(),
		)
	}
	if last.StateRoot != carry.TransitionInput.Checkpoint.StateRoot {
		return fmt.Errorf(
			"checkpoint state root mismatch: block=%s checkpoint=%s",
			last.StateRoot.Hex(),
			carry.TransitionInput.Checkpoint.StateRoot.Hex(),
		)
	}
	return nil
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
