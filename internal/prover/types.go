package prover

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type ValidatedRequest struct {
	Request     protocol.ShastaRequest
	Carry       CarryView
	Blocks      []BlockView
	LogMetadata RequestLogMetadata
}

type ValidatedAggregateRequest struct {
	Request protocol.ShastaAggregateRequest
	Proofs  []AggregateProofView
}

type RequestLogMetadata struct {
	Schema     string
	ChainID    uint64
	BlockCount int
}

type ValidationError struct {
	Err      error
	Metadata RequestLogMetadata
}

func (e *ValidationError) Error() string {
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

type CarryView struct {
	ChainID         uint64
	Verifier        common.Address
	TransitionInput TransitionInputView
}

type TransitionInputView struct {
	ProposalID         uint64
	ProposalHash       common.Hash
	ParentProposalHash common.Hash
	ParentBlockHash    common.Hash
	ActualProver       common.Address
	Transition         TransitionView
	Checkpoint         CheckpointView
}

type TransitionView struct {
	Proposer  common.Address
	Timestamp uint64
}

type CheckpointView struct {
	BlockNumber uint64
	BlockHash   common.Hash
	StateRoot   common.Hash
}

type BlockView struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	StateRoot    common.Hash
	ReceiptsRoot common.Hash
}

type AggregateProofView struct {
	InputHash       common.Hash
	ProofBytes      []byte
	InstanceID      uint32
	InstanceAddress common.Address
	Signature       []byte
	RawCarry        json.RawMessage
	Carry           CarryView
}

type ReplayWitness struct {
	Witness *stateless.Witness
}

type CompactAncestor struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Timestamp  uint64
}

type decodedSignature struct {
	V *big.Int
	R *big.Int
	S *big.Int
}
