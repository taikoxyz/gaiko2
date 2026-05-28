package prover

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func (s ReplayService) Aggregate(
	_ context.Context,
	req *ValidatedAggregateRequest,
) (protocol.ProofResult, error) {
	return aggregateWithSigner(s.signer, req)
}

func aggregateWithSigner(
	signer ProofSigner,
	req *ValidatedAggregateRequest,
) (protocol.ProofResult, error) {
	identity, err := signer.Identity()
	if err != nil {
		return protocol.ProofResult{}, err
	}

	rawCarries := make([]json.RawMessage, 0, len(req.Proofs))
	for _, proof := range req.Proofs {
		rawCarries = append(rawCarries, proof.RawCarry)
	}
	if len(req.Proofs) > 0 {
		first := req.Proofs[0]
		if first.InstanceID != identity.InstanceID {
			return protocol.ProofResult{}, fmt.Errorf(
				"aggregate subproof instance id mismatch: got %d expected %d",
				first.InstanceID,
				identity.InstanceID,
			)
		}
		if first.InstanceAddress != identity.InstanceAddress {
			return protocol.ProofResult{}, fmt.Errorf(
				"aggregate subproof instance address mismatch: got %s expected %s",
				first.InstanceAddress.Hex(),
				identity.InstanceAddress.Hex(),
			)
		}
	}
	aggregationHash, err := hashShastaAggregationInput(rawCarries, identity.InstanceAddress)
	if err != nil {
		return protocol.ProofResult{}, err
	}

	if err := validateAggregateProofSignatures(req.Proofs, identity.InstanceID, identity.InstanceAddress); err != nil {
		return protocol.ProofResult{}, err
	}

	output, err := signer.SignHash(aggregationHash)
	if err != nil {
		return protocol.ProofResult{}, err
	}
	return proofResultFromSignerOutput(aggregationHash, output), nil
}

func validateAggregateProofSignatures(
	proofs []AggregateProofView,
	expectedInstanceID uint32,
	expectedInstance common.Address,
) error {
	for index, proof := range proofs {
		if proof.InstanceID != expectedInstanceID {
			return fmt.Errorf(
				"aggregate proof %d instance id mismatch: got %d expected %d",
				index,
				proof.InstanceID,
				expectedInstanceID,
			)
		}
		if proof.InstanceAddress != expectedInstance {
			return fmt.Errorf(
				"aggregate proof %d instance mismatch: got %s expected %s",
				index,
				proof.InstanceAddress.Hex(),
				expectedInstance.Hex(),
			)
		}

		recovered, err := sigToAddress(proof.InputHash, proof.Signature)
		if err != nil {
			return fmt.Errorf("recover aggregate proof %d signer: %w", index, err)
		}
		if recovered != expectedInstance {
			return fmt.Errorf(
				"aggregate proof %d signer mismatch: got %s expected %s",
				index,
				recovered.Hex(),
				expectedInstance.Hex(),
			)
		}
	}
	return nil
}

func sigToAddress(inputHash common.Hash, sig []byte) (common.Address, error) {
	if len(sig) != 65 {
		return common.Address{}, fmt.Errorf("signature length mismatch: got %d expected 65", len(sig))
	}
	normalized := append([]byte(nil), sig...)
	if normalized[64] >= 27 {
		normalized[64] -= 27
	}
	pubkey, err := crypto.SigToPub(inputHash.Bytes(), normalized)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pubkey), nil
}
