package prover

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
)

func ancestorChainWitness(parent, grandparent *types.Header) *ReplayWitness {
	return &ReplayWitness{Witness: &stateless.Witness{Headers: []*types.Header{parent, grandparent}}}
}

// A well-formed full-ancestor chain (parent-first, hash-linked) is accepted.
func TestValidateReplayWitnessAcceptsFullAncestorChain(t *testing.T) {
	grandparent := &types.Header{Number: big.NewInt(40), Root: common.HexToHash(testHash("a0")), ParentHash: common.HexToHash(testHash("9f"))}
	parent := &types.Header{Number: big.NewInt(41), Root: common.HexToHash(testHash("a1")), ParentHash: grandparent.Hash()}
	child := &types.Header{Number: big.NewInt(42), Root: common.HexToHash(testHash("a2")), ParentHash: parent.Hash()}
	block := types.NewBlockWithHeader(child)

	if err := validateReplayWitness(block, ancestorChainWitness(parent, grandparent)); err != nil {
		t.Fatalf("expected valid full ancestor chain, got %v", err)
	}
}

// A grandparent whose recomputed hash does not match parent.ParentHash is rejected;
// this is the old spoof, now caught because the hash is recomputed, not trusted.
func TestValidateReplayWitnessRejectsBrokenAncestorLinkage(t *testing.T) {
	grandparent := &types.Header{Number: big.NewInt(40), Root: common.HexToHash(testHash("a0")), ParentHash: common.HexToHash(testHash("9f"))}
	parent := &types.Header{Number: big.NewInt(41), Root: common.HexToHash(testHash("a1")), ParentHash: common.HexToHash(testHash("de"))} // wrong: != grandparent.Hash()
	child := &types.Header{Number: big.NewInt(42), Root: common.HexToHash(testHash("a2")), ParentHash: parent.Hash()}
	block := types.NewBlockWithHeader(child)

	if err := validateReplayWitness(block, ancestorChainWitness(parent, grandparent)); err == nil {
		t.Fatalf("expected broken ancestor linkage to be rejected")
	}
}
