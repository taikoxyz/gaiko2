package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/prover"
)

type fakeService struct {
	result protocol.ProofResult
	err    error
}

func (f fakeService) Prove(context.Context, *prover.ValidatedRequest) (protocol.ProofResult, error) {
	return f.result, f.err
}

func TestNewServerReturnsValidationErrorEnvelope(t *testing.T) {
	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewBufferString(`{
		"schema":"v2",
		"payload":{"chain_id":1,"blocks":[],"proof_carry_data":{}}
	}`))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != protocol.ProofStatusError {
		t.Fatalf("unexpected response status: %s", resp.Status)
	}
	if resp.Error == nil || resp.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
}

func TestNewServerReturnsSuccessEnvelope(t *testing.T) {
	server := NewServer(fakeService{
		result: protocol.ProofResult{
			Input: "0xinput",
		},
	})

	parentHash := testHash("11")
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewBufferString(fmt.Sprintf(`{
			"schema":"v1",
			"payload":{
				"chain_id":167013,
				"blocks":[
					{"block":%s},
					{"block":%s}
				],
				"proof_carry_data":{
					"chain_id":167013,
					"transition_input":{
						"parent_block_hash":%q,
						"checkpoint":{"blockNumber":"0x2b","blockHash":%q,"stateRoot":%q}
					}
				}
			}
		}`,
		sampleReplayBlockJSON("0x2a", parentHash, testHash("aa"), testHash("de")),
		sampleReplayBlockJSON("0x2b", testHash("22"), testHash("bb"), testHash("be")),
		parentHash,
		testHash("33"),
		testHash("bb"),
	)))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != protocol.ProofStatusOK {
		t.Fatalf("unexpected response status: %s", resp.Status)
	}
	if resp.Result == nil || resp.Result.Input != "0xinput" {
		t.Fatalf("unexpected result payload: %+v", resp.Result)
	}
}

func sampleReplayBlockJSON(number string, parentHash string, stateRoot string, receiptsRoot string) string {
	return fmt.Sprintf(`{
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
	}`, parentHash, testHash("1d"), testAddress("00"), stateRoot, testHash("56"), receiptsRoot, testBloom(), number, testHash("00"))
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
