package prover

import (
	"fmt"
	"strings"
	"testing"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestValidateRequestAcceptsContiguousPacket(t *testing.T) {
	parentHash := testHash("11")
	lastStateRoot := testHash("bb")
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks: []protocol.ReplayBlock{
				{
					Block: sampleReplayBlock(t, "0x2a", parentHash, testHash("aa"), testHash("de")),
				},
				{
					Block: sampleReplayBlock(t, "0x2b", testHash("22"), lastStateRoot, testHash("be")),
				},
			},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 167013,
				"transition_input": {
					"parent_block_hash": %q,
					"checkpoint": {
						"blockNumber": "0x2b",
						"blockHash": %q,
						"stateRoot": %q
					}
				}
			}`, parentHash, testHash("33"), lastStateRoot)),
		},
	}

	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}

	if validated.Carry.ChainID != 167013 {
		t.Fatalf("unexpected carry chain id: %d", validated.Carry.ChainID)
	}
	if len(validated.Blocks) != 2 {
		t.Fatalf("unexpected block count: %d", len(validated.Blocks))
	}
	if validated.Blocks[1].Number != 43 {
		t.Fatalf("unexpected last block number: %d", validated.Blocks[1].Number)
	}
}

func TestValidateRequestRejectsUnsupportedSchema(t *testing.T) {
	_, err := ValidateRequest(protocol.ShastaRequest{
		Schema: "gaiko2-shasta-v2",
	})
	if err == nil || err.Error() != `unsupported schema "gaiko2-shasta-v2"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequestRejectsNonContiguousBlocks(t *testing.T) {
	parentHash := testHash("11")
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 1,
			Blocks: []protocol.ReplayBlock{
				{Block: sampleReplayBlock(t, "0x2a", parentHash, testHash("aa"), testHash("bb"))},
				{Block: sampleReplayBlock(t, "0x2c", testHash("22"), testHash("cc"), testHash("dd"))},
			},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 1,
				"transition_input": {
					"parent_block_hash": %q,
					"checkpoint": {
						"blockNumber": "0x2c",
						"blockHash": %q,
						"stateRoot": %q
					}
				}
			}`, parentHash, testHash("44"), testHash("cc"))),
		},
	}

	_, err := ValidateRequest(req)
	if err == nil || err.Error() != "block numbers must be contiguous: got 44 after 42" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func sampleReplayBlock(
	t *testing.T,
	number string,
	parentHash string,
	stateRoot string,
	receiptsRoot string,
) []byte {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"header": {
			"parentHash": %q,
			"sha3Uncles": %q,
			"miner": %q,
			"stateRoot": %q,
			"transactionsRoot": %q,
			"receiptsRoot": %q,
			"logsBloom": %q,
			"difficulty": "0x0",
			"number": %q,
			"gasLimit": "0x0",
			"gasUsed": "0x0",
			"timestamp": "0x0",
			"extraData": "0x",
			"mixHash": %q,
			"nonce": "0x0000000000000000",
			"baseFeePerGas": "0x1"
		},
		"body": {
			"transactions": [],
			"ommers": [],
			"withdrawals": []
		}
	}`, parentHash, testHash("1d"), testAddress("00"), stateRoot, testHash("56"), receiptsRoot, testBloom(), number, testHash("00")))
}

func testHash(bytePair string) string {
	return "0x" + strings.Repeat(bytePair, 32)
}

func testAddress(bytePair string) string {
	return "0x" + strings.Repeat(bytePair, 20)
}

func testBloom() string {
	return "0x" + strings.Repeat("00", 256)
}
