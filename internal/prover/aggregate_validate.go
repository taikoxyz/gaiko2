package prover

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func ValidateAggregateRequest(req protocol.ShastaAggregateRequest) (*ValidatedAggregateRequest, error) {
	if req.Schema != protocol.ShastaAggregateRequestSchemaV1 {
		return nil, fmt.Errorf("unsupported schema %q", req.Schema)
	}
	if len(req.Payload.Proofs) == 0 {
		return nil, fmt.Errorf("request must include at least one aggregate proof")
	}

	proofs := make([]AggregateProofView, 0, len(req.Payload.Proofs))
	carries := make([]CarryView, 0, len(req.Payload.Proofs))

	for index, item := range req.Payload.Proofs {
		if strings.TrimSpace(item.Input) == "" {
			return nil, fmt.Errorf("aggregate proof %d is missing input", index)
		}
		if strings.TrimSpace(item.Proof) == "" {
			return nil, fmt.Errorf("aggregate proof %d is missing proof", index)
		}

		carry, err := decodeCarry(item.ProofCarryData)
		if err != nil {
			return nil, fmt.Errorf("decode proof_carry_data for proof %d: %w", index, err)
		}

		inputHash, err := parseHashHex(item.Input)
		if err != nil {
			return nil, fmt.Errorf("decode aggregate proof %d input: %w", index, err)
		}
		expectedHash, err := hashShastaSubproofInput(item.ProofCarryData)
		if err != nil {
			return nil, fmt.Errorf("hash proof_carry_data for proof %d: %w", index, err)
		}
		if inputHash != expectedHash {
			return nil, fmt.Errorf(
				"aggregate proof %d input mismatch: got %s expected %s",
				index,
				inputHash.Hex(),
				expectedHash.Hex(),
			)
		}

		proofBytes, err := parseHexBytes(item.Proof)
		if err != nil {
			return nil, fmt.Errorf("decode aggregate proof %d: %w", index, err)
		}
		instanceID, instanceAddress, signature, err := decodeOneshotProof(proofBytes)
		if err != nil {
			return nil, fmt.Errorf("decode aggregate proof %d metadata: %w", index, err)
		}

		carries = append(carries, carry)
		proofs = append(proofs, AggregateProofView{
			InputHash:       inputHash,
			ProofBytes:      proofBytes,
			InstanceID:      instanceID,
			InstanceAddress: instanceAddress,
			Signature:       signature,
			RawCarry:        item.ProofCarryData,
			Carry:           carry,
		})
	}

	if !validateShastaProofCarryDataVec(carries) {
		return nil, fmt.Errorf("invalid shasta proof carry data")
	}
	expectedInstanceID := proofs[0].InstanceID
	expectedInstanceAddress := proofs[0].InstanceAddress
	for index, proof := range proofs[1:] {
		if proof.InstanceID != expectedInstanceID {
			return nil, fmt.Errorf(
				"aggregate proof %d instance id mismatch: got %d expected %d",
				index+1,
				proof.InstanceID,
				expectedInstanceID,
			)
		}
		if proof.InstanceAddress != expectedInstanceAddress {
			return nil, fmt.Errorf(
				"aggregate proof %d instance address mismatch: got %s expected %s",
				index+1,
				proof.InstanceAddress.Hex(),
				expectedInstanceAddress.Hex(),
			)
		}
	}

	return &ValidatedAggregateRequest{
		Request: req,
		Proofs:  proofs,
	}, nil
}

func validateShastaProofCarryDataVec(carries []CarryView) bool {
	if len(carries) == 0 {
		return false
	}
	expectedProver := carries[0].TransitionInput.ActualProver
	for _, item := range carries {
		if item.TransitionInput.ActualProver != expectedProver {
			return false
		}
	}
	for i := 1; i < len(carries); i++ {
		prev := carries[i-1]
		next := carries[i]
		if prev.TransitionInput.ProposalID+1 != next.TransitionInput.ProposalID {
			return false
		}
		if prev.TransitionInput.ProposalHash != next.TransitionInput.ParentProposalHash {
			return false
		}
		if prev.ChainID != next.ChainID {
			return false
		}
		if prev.Verifier != next.Verifier {
			return false
		}
		if prev.TransitionInput.Checkpoint.BlockHash != next.TransitionInput.ParentBlockHash {
			return false
		}
	}
	return true
}

func parseHashHex(value string) (common.Hash, error) {
	decoded, err := parseHexBytes(value)
	if err != nil {
		return common.Hash{}, err
	}
	if len(decoded) != common.HashLength {
		return common.Hash{}, fmt.Errorf("expected %d bytes, got %d", common.HashLength, len(decoded))
	}
	return common.BytesToHash(decoded), nil
}

func parseHexBytes(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("empty hex string")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(trimmed, "0x"))
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
