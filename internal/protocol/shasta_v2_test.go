package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestShastaV2SchemaConstant(t *testing.T) {
	if ShastaRequestSchemaV2 != "raiko2-shasta-request-v2" {
		t.Fatalf("unexpected v2 request schema constant: %s", ShastaRequestSchemaV2)
	}
}

func TestShastaV2RoundTripUsesGuestInputPayload(t *testing.T) {
	req := ShastaRequestV2{
		Schema: ShastaRequestSchemaV2,
		Payload: ShastaPayloadV2{
			GuestInput: ShastaGuestInput{
				Witnesses:               []json.RawMessage{mustRawMessage(t, `{"block":{"number":"0x2a"}}`)},
				Taiko:                   mustRawMessage(t, `{"proposal_id":"0x1"}`),
				ProposalAncestorHeaders: []json.RawMessage{mustRawMessage(t, `{"number":"0x29"}`)},
				ProposalStateNodes:      []json.RawMessage{mustRawMessage(t, `{"key":"0x01"}`)},
				ProofCarryData:          mustRawMessage(t, `{"chain_id":167013}`),
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal v2 request: %v", err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal v2 envelope: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(envelope["payload"], &payload); err != nil {
		t.Fatalf("unmarshal v2 payload: %v", err)
	}
	if _, ok := payload["guest_input"]; !ok {
		t.Fatalf("v2 payload missing guest_input: %s", data)
	}

	var decoded ShastaRequestV2
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal v2 request: %v", err)
	}

	if decoded.Schema != ShastaRequestSchemaV2 {
		t.Fatalf("unexpected v2 schema: %s", decoded.Schema)
	}
	if len(decoded.Payload.GuestInput.Witnesses) != 1 {
		t.Fatalf("unexpected witness count: %d", len(decoded.Payload.GuestInput.Witnesses))
	}
}

func TestShastaV2DecodePreservesGuestInputFields(t *testing.T) {
	data := []byte(`{
		"schema":"raiko2-shasta-request-v2",
		"payload":{
			"guest_input":{
				"witnesses":[{"block":{"number":"0x2a"}},{"block":{"number":"0x2b"}}],
				"taiko":{"proposal_id":"0x1","prover_data":{"actual_prover":"0xabc"}},
				"proposal_ancestor_headers":[{"number":"0x28"},{"number":"0x29"}],
				"proposal_state_nodes":[{"key":"0x01"},{"key":"0x02"}],
				"proof_carry_data":{"chain_id":167013,"transition_input":{"proposal_id":"0x1"}}
			}
		}
	}`)

	var decoded ShastaRequestV2
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal v2 request: %v", err)
	}

	guestInput := decoded.Payload.GuestInput
	if len(guestInput.Witnesses) != 2 {
		t.Fatalf("unexpected witness count: %d", len(guestInput.Witnesses))
	}
	if len(guestInput.ProposalAncestorHeaders) != 2 {
		t.Fatalf("unexpected proposal ancestor header count: %d", len(guestInput.ProposalAncestorHeaders))
	}
	if len(guestInput.ProposalStateNodes) != 2 {
		t.Fatalf("unexpected proposal state node count: %d", len(guestInput.ProposalStateNodes))
	}
	assertRawMessage(t, guestInput.Witnesses[0], `{"block":{"number":"0x2a"}}`)
	assertRawMessage(t, guestInput.Witnesses[1], `{"block":{"number":"0x2b"}}`)
	assertRawMessage(t, guestInput.Taiko, `{"proposal_id":"0x1","prover_data":{"actual_prover":"0xabc"}}`)
	assertRawMessage(t, guestInput.ProposalAncestorHeaders[0], `{"number":"0x28"}`)
	assertRawMessage(t, guestInput.ProposalAncestorHeaders[1], `{"number":"0x29"}`)
	assertRawMessage(t, guestInput.ProposalStateNodes[0], `{"key":"0x01"}`)
	assertRawMessage(t, guestInput.ProposalStateNodes[1], `{"key":"0x02"}`)
	assertRawMessage(t, guestInput.ProofCarryData, `{"chain_id":167013,"transition_input":{"proposal_id":"0x1"}}`)
}

func TestShastaV1ConstantsRemainStable(t *testing.T) {
	if ShastaRequestSchemaV1 != "raiko2-shasta-request-v1" {
		t.Fatalf("unexpected v1 request schema constant: %s", ShastaRequestSchemaV1)
	}
	if ShastaAggregateRequestSchemaV1 != "raiko2-shasta-aggregate-request-v1" {
		t.Fatalf("unexpected v1 aggregate schema constant: %s", ShastaAggregateRequestSchemaV1)
	}
	if ProofSchemaV1 != "raiko2-proof-v1" {
		t.Fatalf("unexpected v1 proof schema constant: %s", ProofSchemaV1)
	}
}

func assertRawMessage(t *testing.T, actual json.RawMessage, expected string) {
	t.Helper()

	if !bytes.Equal(actual, mustRawMessage(t, expected)) {
		t.Fatalf("unexpected raw message:\nactual:   %s\nexpected: %s", actual, expected)
	}
}
