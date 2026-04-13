package protocol

import (
	"encoding/json"
	"testing"
)

func TestShastaV1RoundTrip(t *testing.T) {
	req := ShastaRequest{
		Schema: ShastaSchemaV1,
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

	if decoded.Schema != ShastaSchemaV1 {
		t.Fatalf("unexpected schema: %s", decoded.Schema)
	}
	if decoded.Payload.ChainID != 167013 {
		t.Fatalf("unexpected chain id: %d", decoded.Payload.ChainID)
	}
	if len(decoded.Payload.Blocks) != 1 {
		t.Fatalf("unexpected block count: %d", len(decoded.Payload.Blocks))
	}
}

func TestProofResponseHelpers(t *testing.T) {
	proof := "0xproof"
	input := "0xinput"
	ok := Success(ProofResult{
		Proof: &proof,
		Input: input,
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
	if decodedOK.Status != ProofStatusOK {
		t.Fatalf("unexpected proof status: %s", decodedOK.Status)
	}
	if decodedOK.Result == nil || decodedOK.Result.Input != input {
		t.Fatalf("unexpected proof result: %+v", decodedOK.Result)
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
