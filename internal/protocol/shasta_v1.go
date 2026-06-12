package protocol

import "encoding/json"

const (
	ShastaRequestSchemaV1                 = "raiko2-shasta-request-v1"
	ShastaAggregateRequestSchemaV1        = "raiko2-shasta-aggregate-request-v1"
	ShastaDirectAggregateRequestSchemaV1  = "raiko2-shasta-direct-aggregate-request-v1"
	RethTDXDirectAggregateRequestSchemaV1 = "reth-tdx-shasta-direct-aggregate-request-v1"
	ProofSchemaV1                         = "raiko2-proof-v1"
	RethTDXProofSchemaV1                  = "reth-tdx-proof-v1"
)

type ShastaRequest struct {
	Schema  string        `json:"schema"`
	Payload ShastaPayload `json:"payload"`
}

type ShastaAggregateRequest struct {
	Schema  string                 `json:"schema"`
	Payload ShastaAggregatePayload `json:"payload"`
}

type ShastaDirectAggregateRequest struct {
	Schema  string                       `json:"schema"`
	Payload ShastaDirectAggregatePayload `json:"payload"`
}

type ShastaPayload struct {
	ChainID        uint64          `json:"chain_id"`
	Blocks         []ReplayBlock   `json:"blocks"`
	ProofCarryData json.RawMessage `json:"proof_carry_data"`
}

type ShastaAggregatePayload struct {
	Proofs []AggregateProof `json:"proofs"`
}

type ShastaDirectAggregatePayload struct {
	Proposals []DirectAggregateProposal `json:"proposals"`
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

type DirectAggregateProposal struct {
	ChainID            uint64                    `json:"chain_id"`
	Verifier           string                    `json:"verifier"`
	ProposalID         uint64                    `json:"proposal_id"`
	ProposalHash       string                    `json:"proposal_hash"`
	ParentProposalHash string                    `json:"parent_proposal_hash"`
	ActualProver       string                    `json:"actual_prover"`
	Transition         DirectAggregateTransition `json:"transition"`
	L2BlockNumbers     []uint64                  `json:"l2_block_numbers"`
}

type DirectAggregateTransition struct {
	Proposer  string `json:"proposer"`
	Timestamp uint64 `json:"timestamp"`
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
	Proof             *string           `json:"proof,omitempty"`
	Quote             *string           `json:"quote,omitempty"`
	PublicKey         *string           `json:"public_key,omitempty"`
	InstanceAddress   *string           `json:"instance_address,omitempty"`
	Input             string            `json:"input"`
	ProofCarryDataVec []json.RawMessage `json:"proof_carry_data_vec,omitempty"`
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
