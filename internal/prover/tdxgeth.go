package prover

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type TDXGethService struct {
	headers L2HeaderSource
	signer  ProofSigner
}

func NewTDXGethService(headers L2HeaderSource, signer ProofSigner) TDXGethService {
	return TDXGethService{
		headers: headers,
		signer:  signer,
	}
}

func (s TDXGethService) Prove(
	ctx context.Context,
	req *ValidatedRequest,
) (protocol.ProofResult, error) {
	if s.headers == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth local L2 header source is not configured")
	}
	if s.signer == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth signer is not configured")
	}

	if err := s.verifyLocalHeaders(ctx, req.Blocks); err != nil {
		return protocol.ProofResult{}, err
	}

	inputHash, err := hashShastaSubproofInput(req.Request.Payload.ProofCarryData)
	if err != nil {
		return protocol.ProofResult{}, err
	}
	return buildProofResult(inputHash, s.signer)
}

func (s TDXGethService) Aggregate(
	_ context.Context,
	req *ValidatedAggregateRequest,
) (protocol.ProofResult, error) {
	return aggregateWithSigner(s.signer, req)
}

func (s TDXGethService) DirectAggregate(
	ctx context.Context,
	req *ValidatedDirectAggregateRequest,
) (protocol.ProofResult, error) {
	if s.headers == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth local L2 header source is not configured")
	}
	if s.signer == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth signer is not configured")
	}

	rawCarries, carries, err := s.buildDirectAggregateCarries(ctx, req.Proposals)
	if err != nil {
		return protocol.ProofResult{}, err
	}
	if !validateShastaProofCarryDataVec(carries) {
		return protocol.ProofResult{}, fmt.Errorf("invalid shasta proof carry data")
	}

	identity, err := s.signer.Identity()
	if err != nil {
		return protocol.ProofResult{}, err
	}
	aggregationHash, err := hashShastaAggregationCarries(carries, identity.InstanceAddress)
	if err != nil {
		return protocol.ProofResult{}, err
	}
	output, err := s.signer.SignHash(aggregationHash)
	if err != nil {
		return protocol.ProofResult{}, err
	}

	result := proofResultFromSignerOutput(aggregationHash, output)
	result.ProofCarryDataVec = rawCarries
	return result, nil
}

func (s TDXGethService) verifyLocalHeaders(ctx context.Context, blocks []BlockView) error {
	for _, expected := range blocks {
		actual, err := s.headers.HeaderByNumber(ctx, expected.Number)
		if err != nil {
			return fmt.Errorf("fetch local L2 block %d: %w", expected.Number, err)
		}
		if actual.Number != expected.Number {
			return fmt.Errorf(
				"local L2 block number mismatch: got %d expected %d",
				actual.Number,
				expected.Number,
			)
		}
		if err := compareHash("block hash", expected.Number, actual.Hash, expected.Hash); err != nil {
			return err
		}
		if err := compareHash("parent hash", expected.Number, actual.ParentHash, expected.ParentHash); err != nil {
			return err
		}
		if err := compareHash("state root", expected.Number, actual.StateRoot, expected.StateRoot); err != nil {
			return err
		}
		if err := compareHash("receipts root", expected.Number, actual.ReceiptsRoot, expected.ReceiptsRoot); err != nil {
			return err
		}
	}
	return nil
}

func (s TDXGethService) buildDirectAggregateCarries(
	ctx context.Context,
	proposals []DirectAggregateProposalView,
) ([]json.RawMessage, []CarryView, error) {
	rawCarries := make([]json.RawMessage, 0, len(proposals))
	carries := make([]CarryView, 0, len(proposals))
	for index, proposal := range proposals {
		carry, err := s.buildDirectAggregateCarry(ctx, index, proposal)
		if err != nil {
			return nil, nil, err
		}
		raw, err := marshalCarry(carry)
		if err != nil {
			return nil, nil, err
		}
		rawCarries = append(rawCarries, raw)
		carries = append(carries, carry)
	}
	return rawCarries, carries, nil
}

func (s TDXGethService) buildDirectAggregateCarry(
	ctx context.Context,
	proposalIndex int,
	proposal DirectAggregateProposalView,
) (CarryView, error) {
	var first L2Header
	var last L2Header
	var previous *L2Header
	for blockIndex, number := range proposal.L2BlockNumbers {
		header, err := s.headers.HeaderByNumber(ctx, number)
		if err != nil {
			return CarryView{}, fmt.Errorf("fetch local L2 block %d: %w", number, err)
		}
		if header.Number != number {
			return CarryView{}, fmt.Errorf(
				"local L2 block number mismatch: got %d expected %d",
				header.Number,
				number,
			)
		}
		if err := requireHeaderProposalID("direct aggregate", header, proposal.ProposalID); err != nil {
			return CarryView{}, err
		}
		if previous != nil && header.ParentHash != previous.Hash {
			return CarryView{}, fmt.Errorf(
				"local L2 parent hash mismatch for direct aggregate proposal %d block %d: got %s expected %s",
				proposalIndex,
				number,
				header.ParentHash.Hex(),
				previous.Hash.Hex(),
			)
		}
		if blockIndex == 0 {
			first = header
		}
		last = header
		previous = &last
	}

	if err := s.verifyDirectAggregateLeftBoundary(ctx, proposal, first); err != nil {
		return CarryView{}, err
	}
	if err := s.verifyDirectAggregateRightBoundary(ctx, proposal, last); err != nil {
		return CarryView{}, err
	}

	return CarryView{
		ChainID:  proposal.ChainID,
		Verifier: proposal.Verifier,
		TransitionInput: TransitionInputView{
			ProposalID:         proposal.ProposalID,
			ProposalHash:       proposal.ProposalHash,
			ParentProposalHash: proposal.ParentProposalHash,
			ParentBlockHash:    first.ParentHash,
			ActualProver:       proposal.ActualProver,
			Transition:         proposal.Transition,
			Checkpoint: CheckpointView{
				BlockNumber: last.Number,
				BlockHash:   last.Hash,
				StateRoot:   last.StateRoot,
			},
		},
	}, nil
}

func (s TDXGethService) verifyDirectAggregateLeftBoundary(
	ctx context.Context,
	proposal DirectAggregateProposalView,
	first L2Header,
) error {
	if first.Number == 0 {
		if proposal.ProposalID != 0 {
			return fmt.Errorf(
				"direct aggregate left boundary missing for proposal %d at genesis block",
				proposal.ProposalID,
			)
		}
		return nil
	}
	if proposal.ProposalID == 0 {
		return fmt.Errorf(
			"direct aggregate left boundary cannot precede proposal 0 at block %d",
			first.Number,
		)
	}

	previousNumber := first.Number - 1
	previous, err := s.headers.HeaderByNumber(ctx, previousNumber)
	if err != nil {
		return fmt.Errorf("fetch direct aggregate left boundary block %d: %w", previousNumber, err)
	}
	if previous.Number != previousNumber {
		return fmt.Errorf(
			"direct aggregate left boundary block number mismatch: got %d expected %d",
			previous.Number,
			previousNumber,
		)
	}
	return requireHeaderProposalID("direct aggregate left boundary", previous, proposal.ProposalID-1)
}

func (s TDXGethService) verifyDirectAggregateRightBoundary(
	ctx context.Context,
	proposal DirectAggregateProposalView,
	last L2Header,
) error {
	if last.Number == ^uint64(0) {
		return fmt.Errorf("direct aggregate right boundary overflows after block %d", last.Number)
	}
	if proposal.ProposalID == ^uint64(0) {
		return fmt.Errorf("direct aggregate right boundary overflows after proposal %d", proposal.ProposalID)
	}

	nextNumber := last.Number + 1
	next, err := s.headers.HeaderByNumber(ctx, nextNumber)
	if err != nil {
		return fmt.Errorf("fetch direct aggregate right boundary block %d: %w", nextNumber, err)
	}
	if next.Number != nextNumber {
		return fmt.Errorf(
			"direct aggregate right boundary block number mismatch: got %d expected %d",
			next.Number,
			nextNumber,
		)
	}
	return requireHeaderProposalID("direct aggregate right boundary", next, proposal.ProposalID+1)
}

func requireHeaderProposalID(label string, header L2Header, expected uint64) error {
	if !header.ProposalIDValid {
		return fmt.Errorf(
			"%s block %d missing Shasta proposal id in extraData",
			label,
			header.Number,
		)
	}
	if header.ProposalID != expected {
		return fmt.Errorf(
			"%s block %d proposal id mismatch: got %d expected %d",
			label,
			header.Number,
			header.ProposalID,
			expected,
		)
	}
	return nil
}

func marshalCarry(carry CarryView) (json.RawMessage, error) {
	raw, err := json.Marshal(struct {
		ChainID         uint64 `json:"chain_id"`
		Verifier        string `json:"verifier"`
		TransitionInput struct {
			ProposalID         uint64 `json:"proposal_id"`
			ProposalHash       string `json:"proposal_hash"`
			ParentProposalHash string `json:"parent_proposal_hash"`
			ParentBlockHash    string `json:"parent_block_hash"`
			ActualProver       string `json:"actual_prover"`
			Transition         struct {
				Proposer  string `json:"proposer"`
				Timestamp uint64 `json:"timestamp"`
			} `json:"transition"`
			Checkpoint struct {
				BlockNumber string `json:"blockNumber"`
				BlockHash   string `json:"blockHash"`
				StateRoot   string `json:"stateRoot"`
			} `json:"checkpoint"`
		} `json:"transition_input"`
	}{
		ChainID:  carry.ChainID,
		Verifier: carry.Verifier.Hex(),
		TransitionInput: struct {
			ProposalID         uint64 `json:"proposal_id"`
			ProposalHash       string `json:"proposal_hash"`
			ParentProposalHash string `json:"parent_proposal_hash"`
			ParentBlockHash    string `json:"parent_block_hash"`
			ActualProver       string `json:"actual_prover"`
			Transition         struct {
				Proposer  string `json:"proposer"`
				Timestamp uint64 `json:"timestamp"`
			} `json:"transition"`
			Checkpoint struct {
				BlockNumber string `json:"blockNumber"`
				BlockHash   string `json:"blockHash"`
				StateRoot   string `json:"stateRoot"`
			} `json:"checkpoint"`
		}{
			ProposalID:         carry.TransitionInput.ProposalID,
			ProposalHash:       carry.TransitionInput.ProposalHash.Hex(),
			ParentProposalHash: carry.TransitionInput.ParentProposalHash.Hex(),
			ParentBlockHash:    carry.TransitionInput.ParentBlockHash.Hex(),
			ActualProver:       carry.TransitionInput.ActualProver.Hex(),
			Transition: struct {
				Proposer  string `json:"proposer"`
				Timestamp uint64 `json:"timestamp"`
			}{
				Proposer:  carry.TransitionInput.Transition.Proposer.Hex(),
				Timestamp: carry.TransitionInput.Transition.Timestamp,
			},
			Checkpoint: struct {
				BlockNumber string `json:"blockNumber"`
				BlockHash   string `json:"blockHash"`
				StateRoot   string `json:"stateRoot"`
			}{
				BlockNumber: quantity(carry.TransitionInput.Checkpoint.BlockNumber),
				BlockHash:   carry.TransitionInput.Checkpoint.BlockHash.Hex(),
				StateRoot:   carry.TransitionInput.Checkpoint.StateRoot.Hex(),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal proof carry data: %w", err)
	}
	return json.RawMessage(raw), nil
}

func compareHash(label string, number uint64, got common.Hash, expected common.Hash) error {
	if got == expected {
		return nil
	}
	return fmt.Errorf(
		"local L2 %s mismatch for block %d: got %s expected %s",
		label,
		number,
		got.Hex(),
		expected.Hex(),
	)
}
