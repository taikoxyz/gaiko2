package prover

import (
	"context"
	"errors"
	"testing"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

// nativeAggregateCarry is a single well-formed subproof carry signed, below, by
// the native mock key.
const nativeAggregateCarry = `{
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
}`

func nativeAggregateRequest(t *testing.T) *ValidatedAggregateRequest {
	t.Helper()

	carry := mustRawMessage(t, nativeAggregateCarry)
	subproofInput, err := hashShastaSubproofInput(carry)
	if err != nil {
		t.Fatalf("subproof input: %v", err)
	}
	subproofResult, err := buildProofResult(subproofInput, NewNativeProofSigner(shastaNativeMockInstance))
	if err != nil {
		t.Fatalf("build subproof result: %v", err)
	}
	validated, err := ValidateAggregateRequest(protocol.ShastaAggregateRequest{
		Schema: protocol.ShastaAggregateRequestSchemaV1,
		Payload: protocol.ShastaAggregatePayload{
			Proofs: []protocol.AggregateProof{{
				Input:          subproofResult.Input,
				Proof:          *subproofResult.Proof,
				ProofCarryData: carry,
			}},
		},
	})
	if err != nil {
		t.Fatalf("validate aggregate request: %v", err)
	}
	return validated
}

// TestConfiguredNativeServiceDisablesAggregate proves the forge-oracle gate: a
// native-mode service built without dev mode refuses to serve the aggregate
// endpoint even for an otherwise valid request, since native mode signs with the
// published mock key.
func TestConfiguredNativeServiceDisablesAggregate(t *testing.T) {
	svc, err := NewConfiguredReplayService(ServiceConfig{Mode: ProvingModeNative}, fakeRunner{})
	if err != nil {
		t.Fatalf("configure native service: %v", err)
	}

	_, err = svc.Aggregate(context.Background(), nativeAggregateRequest(t))
	if !errors.Is(err, ErrAggregateDisabled) {
		t.Fatalf("expected ErrAggregateDisabled in native mode without dev mode, got %v", err)
	}
}

// TestConfiguredNativeServiceDevModeEnablesAggregate confirms the explicit dev
// opt-in re-enables the endpoint for local use.
func TestConfiguredNativeServiceDevModeEnablesAggregate(t *testing.T) {
	svc, err := NewConfiguredReplayService(ServiceConfig{Mode: ProvingModeNative, DevMode: true}, fakeRunner{})
	if err != nil {
		t.Fatalf("configure native dev-mode service: %v", err)
	}

	result, err := svc.Aggregate(context.Background(), nativeAggregateRequest(t))
	if err != nil {
		t.Fatalf("aggregate in native dev mode: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected aggregation proof, got %+v", result)
	}
}

// TestDefaultReplayServiceAllowsAggregate documents that the test/default
// constructor stays permissive, so existing callers and tests are unaffected.
func TestDefaultReplayServiceAllowsAggregate(t *testing.T) {
	_, err := NewReplayService(fakeRunner{}).Aggregate(context.Background(), nativeAggregateRequest(t))
	if errors.Is(err, ErrAggregateDisabled) {
		t.Fatalf("default service must not disable aggregate, got %v", err)
	}
	if err != nil {
		t.Fatalf("aggregate with default service: %v", err)
	}
}
