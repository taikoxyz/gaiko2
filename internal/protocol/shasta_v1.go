package protocol

import "encoding/json"

const (
	ShastaSchemaV1 = "v1"
	ProofSchemaV1  = "gaiko2-proof-v1"
)

type ShastaRequest struct {
	Schema  string        `json:"schema"`
	Payload ShastaPayload `json:"payload"`
}

type ShastaAggregateRequest struct {
	Schema  string                 `json:"schema"`
	Payload ShastaAggregatePayload `json:"payload"`
}

type ShastaPayload struct {
	ChainID        uint64          `json:"chain_id"`
	Blocks         []ReplayBlock   `json:"blocks"`
	ProofCarryData json.RawMessage `json:"proof_carry_data"`
}

type ShastaAggregatePayload struct {
	Proofs []AggregateProof `json:"proofs"`
}

type ReplayBlock struct {
	Block     json.RawMessage `json:"block"`
	ChainSpec json.RawMessage `json:"chain_spec"`
	Witness   json.RawMessage `json:"witness"`
	Accounts  json.RawMessage `json:"accounts"`
}

type AggregateProof struct {
	Input          string          `json:"input"`
	Proof          string          `json:"proof"`
	ProofCarryData json.RawMessage `json:"proof_carry_data"`
}

type ProofResponse struct {
	Schema string       `json:"schema"`
	Status ProofStatus  `json:"status"`
	Result *ProofResult `json:"result,omitempty"`
	Error  *ProofError  `json:"error,omitempty"`
}

type ProofStatus string

const (
	ProofStatusOK    ProofStatus = "ok"
	ProofStatusError ProofStatus = "error"
)

type ProofResult struct {
	Proof           *string `json:"proof,omitempty"`
	Quote           *string `json:"quote,omitempty"`
	PublicKey       *string `json:"public_key,omitempty"`
	InstanceAddress *string `json:"instance_address,omitempty"`
	Input           string  `json:"input"`
}

type ProofError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Success(result ProofResult) ProofResponse {
	return ProofResponse{
		Schema: ProofSchemaV1,
		Status: ProofStatusOK,
		Result: &result,
	}
}

func Failure(err ProofError) ProofResponse {
	return ProofResponse{
		Schema: ProofSchemaV1,
		Status: ProofStatusError,
		Error:  &err,
	}
}
