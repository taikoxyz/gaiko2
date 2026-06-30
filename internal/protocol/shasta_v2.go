package protocol

import "encoding/json"

const ShastaRequestSchemaV2 = "raiko2-shasta-request-v2"

type ShastaRequestV2 struct {
	Schema  string          `json:"schema"`
	Payload ShastaPayloadV2 `json:"payload"`
}

type ShastaPayloadV2 struct {
	GuestInput ShastaGuestInput `json:"guest_input"`
}

type ShastaGuestInput struct {
	Witnesses               []json.RawMessage `json:"witnesses"`
	Taiko                   json.RawMessage   `json:"taiko"`
	ProposalAncestorHeaders []json.RawMessage `json:"proposal_ancestor_headers"`
	ProposalStateNodes      []json.RawMessage `json:"proposal_state_nodes"`
	ProofCarryData          json.RawMessage   `json:"proof_carry_data"`
}
