package prover

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// A compact (hash-only) replay ancestor must be rejected: its hash is
// attacker-controlled and would let BLOCKHASH be spoofed.
func TestDecodeWitnessRejectsCompactAncestor(t *testing.T) {
	witnessJSON := mustRawMessage(t, `{
		"state": [], "state_indices": [], "codes": [],
		"headers": [
			{"number": "0x28", "hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "parent_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "timestamp": "0x0"},
			{"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "header": {
				"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0", "number": "0x29", "gasLimit": "0x0", "gasUsed": "0x0",
				"timestamp": "0x0", "extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000", "baseFeePerGas": "0x1"
			}}
		]
	}`)

	if _, err := decodeWitness(witnessJSON); err == nil {
		t.Fatalf("expected compact replay ancestor to be rejected")
	}
}

// Full ancestors arrive oldest-first (parent last); the decoder must return them
// parent-first so taiko-geth's Root()/Headers[0]==parent invariant holds.
func TestDecodeWitnessOrdersFullAncestorsParentFirst(t *testing.T) {
	witnessJSON := mustRawMessage(t, `{
		"state": [], "state_indices": [], "codes": [],
		"headers": [`+fullWitnessHeaderJSON(41, "0x41")+`,`+fullWitnessHeaderJSON(42, "0x42")+`]
	}`)

	witness, err := decodeWitness(witnessJSON)
	if err != nil {
		t.Fatalf("decode witness: %v", err)
	}
	if len(witness.Witness.Headers) != 2 {
		t.Fatalf("unexpected header count: %d", len(witness.Witness.Headers))
	}
	if witness.Witness.Headers[0].Number.Uint64() != 42 {
		t.Fatalf("parent not first: got number %d", witness.Witness.Headers[0].Number.Uint64())
	}
	if witness.Witness.Root() != witness.Witness.Headers[0].Root {
		t.Fatalf("Root() must equal parent (Headers[0]) root")
	}
}

// With full ancestors, taiko-geth's BLOCKHASH walk returns the real ancestor hashes.
func TestReplayChainContextServesRealAncestorBlockhash(t *testing.T) {
	grandparent := &types.Header{
		Number:     big.NewInt(40),
		Time:       100,
		Root:       common.HexToHash(testHash("a0")),
		ParentHash: common.HexToHash(testHash("9f")),
	}
	parent := &types.Header{
		Number:     big.NewInt(41),
		Time:       101,
		Root:       common.HexToHash(testHash("a1")),
		ParentHash: grandparent.Hash(),
	}
	child := &types.Header{
		Number:     big.NewInt(42),
		Time:       102,
		Root:       common.HexToHash(testHash("a2")),
		ParentHash: parent.Hash(),
	}

	witness := &ReplayWitness{Witness: &stateless.Witness{Headers: []*types.Header{parent, grandparent}}}
	chain := newReplayChainContext(&params.ChainConfig{}, types.NewBlockWithHeader(child), witness)
	getHash := core.GetHashFn(child, chain)

	if got := getHash(41); got != parent.Hash() {
		t.Fatalf("BLOCKHASH(41): got %s want %s", got.Hex(), parent.Hash().Hex())
	}
	if got := getHash(40); got != grandparent.Hash() {
		t.Fatalf("BLOCKHASH(40): got %s want %s", got.Hex(), grandparent.Hash().Hex())
	}
	if got := getHash(39); got != grandparent.ParentHash {
		t.Fatalf("BLOCKHASH(39): got %s want %s", got.Hex(), grandparent.ParentHash.Hex())
	}
}

// fullWitnessHeaderJSON builds a minimal full witness header (with a "header" field)
// at the given number; distinct roots keep hashes unique across numbers.
func fullWitnessHeaderJSON(number uint64, rootPrefix string) string {
	root := rootPrefix + "00000000000000000000000000000000000000000000000000000000000000"
	root = root[:66]
	return `{"header": {
		"parentHash": "0x` + "00" + `00000000000000000000000000000000000000000000000000000000000000",
		"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
		"miner": "0x0000000000000000000000000000000000000000",
		"stateRoot": "` + root + `",
		"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
		"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"difficulty": "0x0", "number": "` + hexUint(number) + `", "gasLimit": "0x0", "gasUsed": "0x0",
		"timestamp": "0x0", "extraData": "0x",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"nonce": "0x0000000000000000", "baseFeePerGas": "0x1"
	}}`
}

func hexUint(v uint64) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0x0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return "0x" + string(buf[i:])
}
