package prover

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var verifyProofWord = func() common.Hash {
	var out [32]byte
	copy(out[:], []byte("VERIFY_PROOF"))
	return common.BytesToHash(out[:])
}()

func hashShastaSubproofInput(raw json.RawMessage) (common.Hash, error) {
	carry, err := decodeCarry(raw)
	if err != nil {
		return common.Hash{}, err
	}
	return hashShastaSubproofCarry(carry), nil
}

func hashShastaSubproofCarry(carry CarryView) common.Hash {
	transitionHash := hashShastaTransitionInput(carry.TransitionInput)

	return hashWords(
		verifyProofWord,
		u64Word(carry.ChainID),
		addressWord(carry.Verifier),
		transitionHash,
	)
}

func hashShastaTransitionInput(input TransitionInputView) common.Hash {
	checkpointHash := hashCheckpoint(input.Checkpoint)
	return hashWords(
		u64Word(input.ProposalID),
		input.ProposalHash,
		input.ParentProposalHash,
		input.ParentBlockHash,
		addressWord(input.ActualProver),
		addressWord(input.Transition.Proposer),
		u48Word(input.Transition.Timestamp),
		checkpointHash,
		u48Word(uint64(input.Checkpoint.BlockNumber)),
		input.Checkpoint.BlockHash,
		input.Checkpoint.StateRoot,
	)
}

func hashCheckpoint(checkpoint CheckpointView) common.Hash {
	return hashWords(
		u48Word(uint64(checkpoint.BlockNumber)),
		checkpoint.BlockHash,
		checkpoint.StateRoot,
	)
}

func hashShastaAggregationInput(
	rawCarries []json.RawMessage,
	instance common.Address,
) (common.Hash, error) {
	if len(rawCarries) == 0 {
		return common.Hash{}, fmt.Errorf("empty shasta aggregation input")
	}

	carries := make([]CarryView, 0, len(rawCarries))
	for index, raw := range rawCarries {
		carry, err := decodeCarry(raw)
		if err != nil {
			return common.Hash{}, fmt.Errorf("decode aggregation proof_carry_data %d: %w", index, err)
		}
		carries = append(carries, carry)
	}

	return hashShastaAggregationCarries(carries, instance)
}

func hashShastaAggregationCarries(
	carries []CarryView,
	instance common.Address,
) (common.Hash, error) {
	commitment, ok := buildShastaCommitmentFromProofCarryDataVec(carries)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid shasta proof carry data")
	}

	first := carries[0]
	commitmentHash := hashCommitment(commitment)
	return hashWords(
		verifyProofWord,
		u64Word(first.ChainID),
		addressWord(first.Verifier),
		commitmentHash,
		addressWord(instance),
	), nil
}

type shastaCommitment struct {
	FirstProposalID              uint64
	FirstProposalParentBlockHash common.Hash
	LastProposalHash             common.Hash
	ActualProver                 common.Address
	EndBlockNumber               uint64
	EndStateRoot                 common.Hash
	Transitions                  []shastaTransition
}

type shastaTransition struct {
	Proposer  common.Address
	Timestamp uint64
	BlockHash common.Hash
}

func buildShastaCommitmentFromProofCarryDataVec(carries []CarryView) (*shastaCommitment, bool) {
	if !validateShastaProofCarryDataVec(carries) {
		return nil, false
	}

	last := carries[len(carries)-1]
	transitions := make([]shastaTransition, 0, len(carries))
	for _, item := range carries {
		transitions = append(transitions, shastaTransition{
			Proposer:  item.TransitionInput.Transition.Proposer,
			Timestamp: item.TransitionInput.Transition.Timestamp,
			BlockHash: item.TransitionInput.Checkpoint.BlockHash,
		})
	}

	return &shastaCommitment{
		FirstProposalID:              carries[0].TransitionInput.ProposalID,
		FirstProposalParentBlockHash: carries[0].TransitionInput.ParentBlockHash,
		LastProposalHash:             last.TransitionInput.ProposalHash,
		ActualProver:                 carries[0].TransitionInput.ActualProver,
		EndBlockNumber:               uint64(last.TransitionInput.Checkpoint.BlockNumber),
		EndStateRoot:                 last.TransitionInput.Checkpoint.StateRoot,
		Transitions:                  transitions,
	}, true
}

func hashCommitment(commitment *shastaCommitment) common.Hash {
	transitionsLen := len(commitment.Transitions)
	values := make([]common.Hash, 0, 9+transitionsLen*3)
	values = append(values,
		u64Word(0x20),
		u64Word(commitment.FirstProposalID),
		commitment.FirstProposalParentBlockHash,
		commitment.LastProposalHash,
		addressWord(commitment.ActualProver),
		u64Word(commitment.EndBlockNumber),
		commitment.EndStateRoot,
		u64Word(0xe0),
		u64Word(uint64(transitionsLen)),
	)

	for _, transition := range commitment.Transitions {
		values = append(values,
			addressWord(transition.Proposer),
			u64Word(transition.Timestamp),
			transition.BlockHash,
		)
	}

	return hashWords(values...)
}

func hashWords(values ...common.Hash) common.Hash {
	data := make([]byte, 0, len(values)*32)
	for _, value := range values {
		data = append(data, value.Bytes()...)
	}
	return crypto.Keccak256Hash(data)
}

func addressWord(addr common.Address) common.Hash {
	return common.BytesToHash(common.LeftPadBytes(addr.Bytes(), 32))
}

func u48Word(value uint64) common.Hash {
	return u64Word(value & 0xffffffffffff)
}

func u64Word(value uint64) common.Hash {
	var out [32]byte
	binary.BigEndian.PutUint64(out[24:], value)
	return common.BytesToHash(out[:])
}

func prefixedHex(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}

func equalHex(a, b string) bool {
	return strings.EqualFold(a, b)
}
