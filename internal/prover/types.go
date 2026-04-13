package prover

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type ValidatedRequest struct {
	Request protocol.ShastaRequest
	Carry   CarryView
	Blocks  []BlockView
}

type ValidatedAggregateRequest struct {
	Request protocol.ShastaAggregateRequest
	Proofs  []AggregateProofView
}

type CarryView struct {
	ChainID         uint64
	ParentBlockHash string
	Checkpoint      CheckpointView
}

type CheckpointView struct {
	BlockNumber uint64
	BlockHash   string
	StateRoot   string
}

type BlockView struct {
	Number       uint64
	ParentHash   string
	StateRoot    string
	ReceiptsRoot string
}

type AggregateProofView struct {
	InputHash    common.Hash
	ProofBytes   []byte
	RawCarry     json.RawMessage
	DecodedCarry rawProofCarryData
}

type ReplayWitness struct {
	Witness          *stateless.Witness
	CompactAncestors []CompactAncestor
}

type CompactAncestor struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Timestamp  uint64
}
