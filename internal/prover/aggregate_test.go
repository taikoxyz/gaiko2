package prover

import (
	"context"
	"encoding/json"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestHashShastaAggregationInputDependsOnCarrySequence(t *testing.T) {
	instance := common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec")
	base, err := hashShastaAggregationInput([]json.RawMessage{
		mustRawMessage(t, `{
			"chain_id": 167013,
			"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
			"transition_input": {
				"proposal_id": 7,
				"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
				"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
				"checkpoint": {
					"blockNumber": "0x2a",
					"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
					"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
				}
			}
		}`),
	}, instance)
	if err != nil {
		t.Fatalf("hash base aggregation input: %v", err)
	}

	diffProposal, err := hashShastaAggregationInput([]json.RawMessage{
		mustRawMessage(t, `{
			"chain_id": 167013,
			"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
			"transition_input": {
				"proposal_id": 8,
				"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
				"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
				"checkpoint": {
					"blockNumber": "0x2a",
					"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
					"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
				}
			}
		}`),
	}, instance)
	if err != nil {
		t.Fatalf("hash changed aggregation input: %v", err)
	}

	if base == diffProposal {
		t.Fatalf("expected aggregation hash to change with proposal sequence, got %s", base)
	}
}

func TestReplayServiceReturnsSignedAggregationProofResult(t *testing.T) {
	carry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 7,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			}
		}
	}`)
	subproofInput, err := hashShastaSubproofInput(carry)
	if err != nil {
		t.Fatalf("subproof input: %v", err)
	}

	subproofResult, err := buildProofResult(subproofInput, NewNativeProofSigner(shastaNativeMockInstance))
	if err != nil {
		t.Fatalf("build subproof result: %v", err)
	}
	req := protocol.ShastaAggregateRequest{
		Schema: protocol.ShastaAggregateRequestSchemaV1,
		Payload: protocol.ShastaAggregatePayload{
			Proofs: []protocol.AggregateProof{
				{
					Input:          subproofResult.Input,
					Proof:          *subproofResult.Proof,
					ProofCarryData: carry,
				},
			},
		},
	}

	validated, err := ValidateAggregateRequest(req)
	if err != nil {
		t.Fatalf("validate aggregate request: %v", err)
	}

	service := NewReplayService(nil)
	result, err := service.Aggregate(context.Background(), validated)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	expectedInput, err := hashShastaAggregationInput(
		[]json.RawMessage{carry},
		common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec"),
	)
	if err != nil {
		t.Fatalf("hash aggregation input: %v", err)
	}
	if result.Input != expectedInput.Hex() {
		t.Fatalf("unexpected aggregation input hash: got %s want %s", result.Input, expectedInput.Hex())
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected aggregation proof, got %+v", result)
	}
	if result.InstanceAddress == nil || *result.InstanceAddress != common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec").Hex() {
		t.Fatalf("unexpected instance address: %+v", result.InstanceAddress)
	}
}

func TestValidateAggregateRequestRejectsMixedInstanceIDs(t *testing.T) {
	firstCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 7,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			}
		}
	}`)
	secondCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 8,
			"proposal_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"parent_proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_block_hash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 124 },
			"checkpoint": {
				"blockNumber": "0x2b",
				"blockHash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"stateRoot": "0x9999999999999999999999999999999999999999999999999999999999999999"
			}
		}
	}`)
	firstInputHash, err := hashShastaSubproofInput(firstCarry)
	if err != nil {
		t.Fatalf("first subproof input: %v", err)
	}
	secondInputHash, err := hashShastaSubproofInput(secondCarry)
	if err != nil {
		t.Fatalf("second subproof input: %v", err)
	}
	first, err := buildProofResult(firstInputHash, NewNativeProofSigner(1))
	if err != nil {
		t.Fatalf("build first proof: %v", err)
	}
	second, err := buildProofResult(secondInputHash, NewNativeProofSigner(2))
	if err != nil {
		t.Fatalf("build second proof: %v", err)
	}

	_, err = ValidateAggregateRequest(protocol.ShastaAggregateRequest{
		Schema: protocol.ShastaAggregateRequestSchemaV1,
		Payload: protocol.ShastaAggregatePayload{
			Proofs: []protocol.AggregateProof{
				{Input: first.Input, Proof: *first.Proof, ProofCarryData: firstCarry},
				{Input: second.Input, Proof: *second.Proof, ProofCarryData: secondCarry},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "instance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAggregateRequestRejectsMixedInstanceAddresses(t *testing.T) {
	firstCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 7,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			}
		}
	}`)
	secondCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 8,
			"proposal_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"parent_proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_block_hash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 124 },
			"checkpoint": {
				"blockNumber": "0x2b",
				"blockHash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"stateRoot": "0x9999999999999999999999999999999999999999999999999999999999999999"
			}
		}
	}`)
	firstInputHash, err := hashShastaSubproofInput(firstCarry)
	if err != nil {
		t.Fatalf("first subproof input: %v", err)
	}
	secondInputHash, err := hashShastaSubproofInput(secondCarry)
	if err != nil {
		t.Fatalf("second subproof input: %v", err)
	}
	first, err := buildProofResult(firstInputHash, NewNativeProofSigner(1))
	if err != nil {
		t.Fatalf("build first proof: %v", err)
	}
	second, err := buildProofResult(secondInputHash, NewNativeProofSigner(1))
	if err != nil {
		t.Fatalf("build second proof: %v", err)
	}
	mutatedProof := mutateProofAddress(t, *second.Proof, common.HexToAddress("0x1212121212121212121212121212121212121212"))

	_, err = ValidateAggregateRequest(protocol.ShastaAggregateRequest{
		Schema: protocol.ShastaAggregateRequestSchemaV1,
		Payload: protocol.ShastaAggregatePayload{
			Proofs: []protocol.AggregateProof{
				{Input: first.Input, Proof: *first.Proof, ProofCarryData: firstCarry},
				{Input: second.Input, Proof: mutatedProof, ProofCarryData: secondCarry},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "instance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mutateProofAddress(t *testing.T, proof string, address common.Address) string {
	t.Helper()
	bytes := common.FromHex(proof)
	if len(bytes) != 89 {
		t.Fatalf("unexpected proof length: %d", len(bytes))
	}
	copy(bytes[4:24], address.Bytes())
	return "0x" + hex.EncodeToString(bytes)
}
