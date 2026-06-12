package prover

import (
	"fmt"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func ValidateDirectAggregateRequest(
	req protocol.ShastaDirectAggregateRequest,
) (*ValidatedDirectAggregateRequest, error) {
	if !isDirectAggregateSchema(req.Schema) {
		return nil, fmt.Errorf("unsupported schema %q", req.Schema)
	}
	if len(req.Payload.Proposals) == 0 {
		return nil, fmt.Errorf("request must include at least one direct aggregate proposal")
	}

	proposals := make([]DirectAggregateProposalView, 0, len(req.Payload.Proposals))
	for index, item := range req.Payload.Proposals {
		view, err := decodeDirectAggregateProposal(item)
		if err != nil {
			return nil, fmt.Errorf("decode direct aggregate proposal %d: %w", index, err)
		}
		if err := validateDirectAggregateBlockNumbers(index, view.L2BlockNumbers); err != nil {
			return nil, err
		}
		if index > 0 {
			if err := validateDirectAggregateProposalContinuity(proposals[index-1], view, index); err != nil {
				return nil, err
			}
		}
		proposals = append(proposals, view)
	}

	return &ValidatedDirectAggregateRequest{
		Request:   req,
		Proposals: proposals,
	}, nil
}

func isDirectAggregateSchema(schema string) bool {
	return schema == protocol.ShastaDirectAggregateRequestSchemaV1 ||
		schema == protocol.RethTDXDirectAggregateRequestSchemaV1
}

func decodeDirectAggregateProposal(
	item protocol.DirectAggregateProposal,
) (DirectAggregateProposalView, error) {
	verifier, err := parseAddressString(item.Verifier)
	if err != nil {
		return DirectAggregateProposalView{}, fmt.Errorf("parse verifier: %w", err)
	}
	proposalHash, err := parseHashString(item.ProposalHash)
	if err != nil {
		return DirectAggregateProposalView{}, fmt.Errorf("parse proposal_hash: %w", err)
	}
	parentProposalHash, err := parseHashString(item.ParentProposalHash)
	if err != nil {
		return DirectAggregateProposalView{}, fmt.Errorf("parse parent_proposal_hash: %w", err)
	}
	actualProver, err := parseAddressString(item.ActualProver)
	if err != nil {
		return DirectAggregateProposalView{}, fmt.Errorf("parse actual_prover: %w", err)
	}
	proposer, err := parseAddressString(item.Transition.Proposer)
	if err != nil {
		return DirectAggregateProposalView{}, fmt.Errorf("parse transition.proposer: %w", err)
	}

	return DirectAggregateProposalView{
		ChainID:            item.ChainID,
		Verifier:           verifier,
		ProposalID:         item.ProposalID,
		ProposalHash:       proposalHash,
		ParentProposalHash: parentProposalHash,
		ActualProver:       actualProver,
		Transition: TransitionView{
			Proposer:  proposer,
			Timestamp: item.Transition.Timestamp,
		},
		L2BlockNumbers: append([]uint64(nil), item.L2BlockNumbers...),
	}, nil
}

func validateDirectAggregateBlockNumbers(index int, numbers []uint64) error {
	if len(numbers) == 0 {
		return fmt.Errorf("direct aggregate proposal %d l2_block_numbers must not be empty", index)
	}
	previous := numbers[0]
	for _, number := range numbers[1:] {
		if number != previous+1 {
			return fmt.Errorf(
				"direct aggregate proposal %d l2_block_numbers must be contiguous: got %d after %d",
				index,
				number,
				previous,
			)
		}
		previous = number
	}
	return nil
}

func validateDirectAggregateProposalContinuity(
	previous DirectAggregateProposalView,
	current DirectAggregateProposalView,
	index int,
) error {
	if current.ProposalID != previous.ProposalID+1 {
		return fmt.Errorf(
			"direct aggregate proposal %d proposal_id must be contiguous: got %d after %d",
			index,
			current.ProposalID,
			previous.ProposalID,
		)
	}
	if current.ParentProposalHash != previous.ProposalHash {
		return fmt.Errorf(
			"direct aggregate proposal %d parent_proposal_hash mismatch: got %s expected %s",
			index,
			current.ParentProposalHash.Hex(),
			previous.ProposalHash.Hex(),
		)
	}
	if current.ChainID != previous.ChainID {
		return fmt.Errorf(
			"direct aggregate proposal %d chain_id mismatch: got %d expected %d",
			index,
			current.ChainID,
			previous.ChainID,
		)
	}
	if current.Verifier != previous.Verifier {
		return fmt.Errorf(
			"direct aggregate proposal %d verifier mismatch: got %s expected %s",
			index,
			current.Verifier.Hex(),
			previous.Verifier.Hex(),
		)
	}
	if current.ActualProver != previous.ActualProver {
		return fmt.Errorf(
			"direct aggregate proposal %d actual_prover mismatch: got %s expected %s",
			index,
			current.ActualProver.Hex(),
			previous.ActualProver.Hex(),
		)
	}

	previousLastBlock := previous.L2BlockNumbers[len(previous.L2BlockNumbers)-1]
	currentFirstBlock := current.L2BlockNumbers[0]
	if currentFirstBlock != previousLastBlock+1 {
		return fmt.Errorf(
			"direct aggregate proposal %d l2_block_numbers must continue previous proposal: got %d after %d",
			index,
			currentFirstBlock,
			previousLastBlock,
		)
	}
	return nil
}
