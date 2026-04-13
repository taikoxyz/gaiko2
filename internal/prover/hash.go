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
	var carry rawProofCarryData
	if err := json.Unmarshal(raw, &carry); err != nil {
		return common.Hash{}, fmt.Errorf("unmarshal proof_carry_data for hashing: %w", err)
	}

	transitionHash, err := hashShastaTransitionInput(carry.TransitionInput)
	if err != nil {
		return common.Hash{}, err
	}

	return hashWords(
		verifyProofWord,
		u64Word(carry.ChainID),
		addressWord(common.HexToAddress(carry.Verifier)),
		transitionHash,
	), nil
}

func hashShastaTransitionInput(input rawTransitionInput) (common.Hash, error) {
	checkpointHash, err := hashCheckpoint(input.Checkpoint)
	if err != nil {
		return common.Hash{}, err
	}

	return hashWords(
		u64Word(input.ProposalID),
		common.HexToHash(input.ProposalHash),
		common.HexToHash(input.ParentProposalHash),
		common.HexToHash(input.ParentBlockHash),
		addressWord(common.HexToAddress(input.ActualProver)),
		addressWord(common.HexToAddress(input.Transition.Proposer)),
		u48Word(input.Transition.Timestamp),
		checkpointHash,
		u48Word(uint64(input.Checkpoint.BlockNumber)),
		common.HexToHash(input.Checkpoint.BlockHash),
		common.HexToHash(input.Checkpoint.StateRoot),
	), nil
}

func hashCheckpoint(checkpoint rawCheckpoint) (common.Hash, error) {
	return hashWords(
		u48Word(uint64(checkpoint.BlockNumber)),
		common.HexToHash(checkpoint.BlockHash),
		common.HexToHash(checkpoint.StateRoot),
	), nil
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
