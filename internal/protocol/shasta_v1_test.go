package protocol

import (
	"encoding/json"
	"testing"
)

func TestShastaV1RoundTrip(t *testing.T) {
	req := ShastaRequest{
		Schema: ShastaRequestSchemaV1,
		Payload: ShastaPayload{
			ChainID: 167013,
			Blocks: []ReplayBlock{
				{
					Block:     mustRawMessage(t, `{"number":"0x2a"}`),
					ChainSpec: mustRawMessage(t, `{"chain_id":167013}`),
					Witness:   mustRawMessage(t, `{"headers":[]}`),
					Accounts:  mustRawMessage(t, `{}`),
				},
			},
			ProofCarryData: mustRawMessage(t, `{"chain_id":167013}`),
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded ShastaRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if decoded.Schema != ShastaRequestSchemaV1 {
		t.Fatalf("unexpected schema: %s", decoded.Schema)
	}
	if ShastaRequestSchemaV1 != "raiko2-shasta-request-v1" {
		t.Fatalf("unexpected request schema constant: %s", ShastaRequestSchemaV1)
	}
	if decoded.Payload.ChainID != 167013 {
		t.Fatalf("unexpected chain id: %d", decoded.Payload.ChainID)
	}
	if len(decoded.Payload.Blocks) != 1 {
		t.Fatalf("unexpected block count: %d", len(decoded.Payload.Blocks))
	}
}

func TestShastaAggregateV1RoundTrip(t *testing.T) {
	req := ShastaAggregateRequest{
		Schema: ShastaAggregateRequestSchemaV1,
		Payload: ShastaAggregatePayload{
			Proofs: []AggregateProof{
				{
					Input:          "0x" + "11",
					Proof:          "0x" + "22",
					ProofCarryData: mustRawMessage(t, `{"chain_id":167013}`),
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded ShastaAggregateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if decoded.Schema != ShastaAggregateRequestSchemaV1 {
		t.Fatalf("unexpected schema: %s", decoded.Schema)
	}
	if ShastaAggregateRequestSchemaV1 != "raiko2-shasta-aggregate-request-v1" {
		t.Fatalf("unexpected aggregate schema constant: %s", ShastaAggregateRequestSchemaV1)
	}
	if len(decoded.Payload.Proofs) != 1 {
		t.Fatalf("unexpected proof count: %d", len(decoded.Payload.Proofs))
	}
	if decoded.Payload.Proofs[0].Input != "0x11" {
		t.Fatalf("unexpected proof input: %s", decoded.Payload.Proofs[0].Input)
	}
}

func TestShastaDirectAggregateV1RoundTrip(t *testing.T) {
	req := ShastaDirectAggregateRequest{
		Schema: ShastaDirectAggregateRequestSchemaV1,
		Payload: ShastaDirectAggregatePayload{
			Proposals: []DirectAggregateProposal{
				{
					ChainID:            167013,
					Verifier:           "0x1111111111111111111111111111111111111111",
					ProposalID:         7,
					ProposalHash:       "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ParentProposalHash: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					ActualProver:       "0x2222222222222222222222222222222222222222",
					Transition: DirectAggregateTransition{
						Proposer:  "0x3333333333333333333333333333333333333333",
						Timestamp: 123,
					},
					L2BlockNumbers: []uint64{42, 43},
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal direct aggregate request: %v", err)
	}

	var decoded ShastaDirectAggregateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal direct aggregate request: %v", err)
	}

	if decoded.Schema != ShastaDirectAggregateRequestSchemaV1 {
		t.Fatalf("unexpected schema: %s", decoded.Schema)
	}
	if ShastaDirectAggregateRequestSchemaV1 != "raiko2-shasta-direct-aggregate-request-v1" {
		t.Fatalf("unexpected direct aggregate schema constant: %s", ShastaDirectAggregateRequestSchemaV1)
	}
	if len(decoded.Payload.Proposals) != 1 {
		t.Fatalf("unexpected proposal count: %d", len(decoded.Payload.Proposals))
	}
	if len(decoded.Payload.Proposals[0].L2BlockNumbers) != 2 {
		t.Fatalf("unexpected block number count: %d", len(decoded.Payload.Proposals[0].L2BlockNumbers))
	}
}

func TestProofResponseHelpers(t *testing.T) {
	proof := "0xproof"
	input := "0xinput"
	carry := json.RawMessage(`{"chain_id":167013}`)
	ok := Success(ProofResult{
		Proof:             &proof,
		Input:             input,
		ProofCarryDataVec: []json.RawMessage{carry},
	})

	data, err := json.Marshal(ok)
	if err != nil {
		t.Fatalf("marshal success response: %v", err)
	}

	var decodedOK ProofResponse
	if err := json.Unmarshal(data, &decodedOK); err != nil {
		t.Fatalf("unmarshal success response: %v", err)
	}

	if decodedOK.Schema != ProofSchemaV1 {
		t.Fatalf("unexpected proof schema: %s", decodedOK.Schema)
	}
	if ProofSchemaV1 != "raiko2-proof-v1" {
		t.Fatalf("unexpected proof schema constant: %s", ProofSchemaV1)
	}
	if decodedOK.Status != ProofStatusOK {
		t.Fatalf("unexpected proof status: %s", decodedOK.Status)
	}
	if decodedOK.Result == nil || decodedOK.Result.Input != input {
		t.Fatalf("unexpected proof result: %+v", decodedOK.Result)
	}
	if len(decodedOK.Result.ProofCarryDataVec) != 1 {
		t.Fatalf("unexpected carry vector: %+v", decodedOK.Result.ProofCarryDataVec)
	}

	fail := Failure(ProofError{
		Code:    "INVALID_SCHEMA",
		Message: "unsupported schema",
	})
	data, err = json.Marshal(fail)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}

	var decodedFail ProofResponse
	if err := json.Unmarshal(data, &decodedFail); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}

	if decodedFail.Status != ProofStatusError {
		t.Fatalf("unexpected error status: %s", decodedFail.Status)
	}
	if decodedFail.Error == nil || decodedFail.Error.Code != "INVALID_SCHEMA" {
		t.Fatalf("unexpected error payload: %+v", decodedFail.Error)
	}
}

func mustRawMessage(t *testing.T, value string) json.RawMessage {
	t.Helper()
	return json.RawMessage(value)
}
