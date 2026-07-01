package prover

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestValidateRequestRejectsLegacyReplayPacketV1(t *testing.T) {
	parentHash := testHash("11")
	lastStateRoot := testHash("bb")
	firstBlock := sampleReplayBlock(t, "0x2a", parentHash, testHash("aa"), testHash("de"))
	firstHash := replayBlockHash(t, firstBlock)
	secondBlock := sampleReplayBlock(t, "0x2b", firstHash, lastStateRoot, testHash("be"))
	secondHash := replayBlockHash(t, secondBlock)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks: []protocol.ReplayBlock{
				{
					Block: firstBlock,
				},
				{
					Block: secondBlock,
				},
			},
			ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2b", secondHash, lastStateRoot),
		},
	}

	_, err := ValidateRequest(req)
	if err == nil || err.Error() != "request must include guest_input" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequestAcceptsGuestInputV1(t *testing.T) {
	input := newManifestBindingFixture(t).view(t).Raw
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			GuestInput: &input,
		},
	}

	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate v1 guest input request: %v", err)
	}

	if validated.Request.Schema != protocol.ShastaRequestSchemaV1 {
		t.Fatalf("unexpected schema: %s", validated.Request.Schema)
	}
	if validated.Request.Payload.ChainID != 167013 {
		t.Fatalf("unexpected chain id: %d", validated.Request.Payload.ChainID)
	}
	if len(validated.Request.Payload.Blocks) != 1 {
		t.Fatalf("unexpected replay block count: %d", len(validated.Request.Payload.Blocks))
	}
	if validated.Carry.TransitionInput.ProposalID != 12345 {
		t.Fatalf("unexpected proposal id: %d", validated.Carry.TransitionInput.ProposalID)
	}
}

func TestValidateRequestV1RejectsManifestMismatch(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.blockTimestamp++
	input := fixture.view(t).Raw
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			GuestInput: &input,
		},
	}

	_, err := ValidateRequest(req)
	if err == nil || !strings.Contains(err.Error(), "manifest block 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequestRejectsUnsupportedSchema(t *testing.T) {
	_, err := ValidateRequest(protocol.ShastaRequest{
		Schema: "raiko2-shasta-request-v2",
	})
	if err == nil || err.Error() != `unsupported schema "raiko2-shasta-request-v2"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBlockViewsRejectsNonContiguousBlocks(t *testing.T) {
	parentHash := testHash("11")
	err := validateReplayBlocksForTest(t,
		[]json.RawMessage{
			sampleReplayBlock(t, "0x2a", parentHash, testHash("aa"), testHash("bb")),
			sampleReplayBlock(t, "0x2c", testHash("22"), testHash("cc"), testHash("dd")),
		},
		sampleCarryData(t, 1, parentHash, "0x2c", testHash("44"), testHash("cc")),
	)
	if err == nil || err.Error() != "block numbers must be contiguous: got 44 after 42" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBlockViewsRejectsBrokenParentHashPropagation(t *testing.T) {
	firstBlock := sampleReplayBlock(t, "0x2a", testHash("11"), testHash("aa"), testHash("de"))
	err := validateReplayBlocksForTest(t,
		[]json.RawMessage{
			firstBlock,
			sampleReplayBlock(t, "0x2b", testHash("22"), testHash("bb"), testHash("be")),
		},
		sampleCarryData(t, 167013, testHash("11"), "0x2b", testHash("33"), testHash("bb")),
	)
	if err == nil || !strings.Contains(err.Error(), "parent hash") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBlockViewsRejectsCheckpointBlockHashMismatch(t *testing.T) {
	firstBlock := sampleReplayBlock(t, "0x2a", testHash("11"), testHash("aa"), testHash("de"))
	firstHash := replayBlockHash(t, firstBlock)
	secondBlock := sampleReplayBlock(t, "0x2b", firstHash, testHash("bb"), testHash("be"))
	err := validateReplayBlocksForTest(t,
		[]json.RawMessage{
			firstBlock,
			secondBlock,
		},
		sampleCarryData(t, 167013, testHash("11"), "0x2b", testHash("44"), testHash("bb")),
	)
	if err == nil || !strings.Contains(err.Error(), "checkpoint block hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeCarryRejectsMalformedVerifier(t *testing.T) {
	block := sampleReplayBlock(t, "0x2a", testHash("11"), testHash("aa"), testHash("de"))
	blockHash := replayBlockHash(t, block)
	_, err := decodeCarry(mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": 167013,
		"verifier": "0x1234",
		"transition_input": {
			"proposal_id": 42,
			"proposal_hash": %q,
			"parent_proposal_hash": %q,
			"parent_block_hash": %q,
			"actual_prover": %q,
			"transition": {
				"proposer": %q,
				"timestamp": 123
			},
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": %q,
				"stateRoot": %q
			}
		}
	}`, testHash("aa"), testHash("bb"), testHash("11"), testAddress("77"), testAddress("22"), blockHash, testHash("aa"))))
	if err == nil || !strings.Contains(err.Error(), "verifier") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func validateReplayBlocksForTest(
	t *testing.T,
	rawBlocks []json.RawMessage,
	rawCarry json.RawMessage,
) error {
	t.Helper()

	carry, err := decodeCarry(rawCarry)
	if err != nil {
		t.Fatalf("decode carry: %v", err)
	}
	blocks := make([]BlockView, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		block, err := decodeBlock(protocol.ReplayBlock{Block: raw})
		if err != nil {
			t.Fatalf("decode replay block: %v", err)
		}
		blocks = append(blocks, block)
	}
	return validateBlockViews(blocks, carry)
}

func TestDecodeHeaderPreservesSlotNumber(t *testing.T) {
	headerRaw := sampleHeaderWithField(t, "slotNumber", `"0x3039"`)

	header, err := decodeHeader(headerRaw)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header.SlotNumber == nil || *header.SlotNumber != 12345 {
		t.Fatalf("unexpected slot number: %v", header.SlotNumber)
	}
}

func TestDecodeHeaderRejectsNonNullBlockAccessListHash(t *testing.T) {
	headerRaw := sampleHeaderWithField(t, "block_access_list_hash", fmt.Sprintf("%q", testHash("44")))

	_, err := decodeHeader(headerRaw)
	if err == nil || err.Error() != "field block_access_list_hash is not supported by taiko-geth replay" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func sampleHeaderWithField(t *testing.T, field string, value string) []byte {
	t.Helper()
	decoded, err := decodeBlockEnvelope(sampleReplayBlock(t, "0x2a", testHash("11"), testHash("aa"), testHash("bb")))
	if err != nil {
		t.Fatalf("decode block envelope: %v", err)
	}
	fields, err := decodeJSONObject(decoded.Header)
	if err != nil {
		t.Fatalf("decode header object: %v", err)
	}
	fields[field] = mustRawMessage(t, value)
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	return raw
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

func sampleCarryData(
	t *testing.T,
	chainID uint64,
	parentHash string,
	checkpointNumber string,
	checkpointBlockHash string,
	stateRoot string,
) []byte {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"verifier": %q,
		"transition_input": {
			"proposal_id": 42,
			"proposal_hash": %q,
			"parent_proposal_hash": %q,
			"parent_block_hash": %q,
			"actual_prover": %q,
			"transition": {
				"proposer": %q,
				"timestamp": 123
			},
			"checkpoint": {
				"blockNumber": %q,
				"blockHash": %q,
				"stateRoot": %q
			}
		}
	}`, chainID, testAddress("f9"), testHash("aa"), testHash("bb"), parentHash, testAddress("77"), testAddress("22"), checkpointNumber, checkpointBlockHash, stateRoot))
}

func replayBlockHash(t *testing.T, raw []byte) string {
	t.Helper()
	decoded, err := decodeBlockEnvelope(raw)
	if err != nil {
		t.Fatalf("decode block envelope: %v", err)
	}
	header, err := decodeHeader(decoded.Header)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	return header.Hash().Hex()
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
