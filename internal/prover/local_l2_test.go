package prover

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalL2RPCFetchesHeaderOnlyBlockByNumber(t *testing.T) {
	var gotMethod string
	var gotParams []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotMethod = req.Method
		gotParams = req.Params
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"number":       "0x2a",
				"hash":         testHash("aa"),
				"parentHash":   testHash("11"),
				"stateRoot":    testHash("bb"),
				"receiptsRoot": testHash("cc"),
				"extraData":    shastaExtraDataHex(7),
			},
		})
	}))
	t.Cleanup(server.Close)

	client, err := NewLocalL2RPC(server.URL)
	if err != nil {
		t.Fatalf("new local l2 rpc: %v", err)
	}
	header, err := client.HeaderByNumber(context.Background(), 42)
	if err != nil {
		t.Fatalf("header by number: %v", err)
	}
	if gotMethod != "eth_getBlockByNumber" {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if len(gotParams) != 2 || gotParams[0] != "0x2a" || gotParams[1] != false {
		t.Fatalf("unexpected params: %#v", gotParams)
	}
	if header.Number != 42 {
		t.Fatalf("unexpected number: %d", header.Number)
	}
	if !header.ProposalIDValid || header.ProposalID != 7 {
		t.Fatalf("unexpected proposal id: valid=%v id=%d", header.ProposalIDValid, header.ProposalID)
	}
}

func TestLocalL2RPCRejectsNonSuccessHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "busy", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client, err := NewLocalL2RPC(server.URL)
	if err != nil {
		t.Fatalf("new local l2 rpc: %v", err)
	}
	_, err = client.HeaderByNumber(context.Background(), 42)
	if err == nil || err.Error() != "local L2 eth_getBlockByNumber(42): eth_getBlockByNumber returned HTTP 502" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalL2RPCFetchesCertainBlockAndOriginByBatchID(t *testing.T) {
	methods := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, req.Method)
		if len(req.Params) != 1 || req.Params[0] != "0x7" {
			t.Fatalf("unexpected params for %s: %#v", req.Method, req.Params)
		}
		switch req.Method {
		case "taikoAuth_lastCertainBlockIDByBatchID":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "0x2a",
			})
		case "taikoAuth_lastCertainL1OriginByBatchID":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"blockID":       "0x2a",
					"l2BlockHash":   testHash("aa"),
					"l1BlockHeight": "0x64",
					"l1BlockHash":   testHash("bb"),
				},
			})
		default:
			t.Fatalf("unexpected method: %s", req.Method)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewLocalL2RPC(server.URL)
	if err != nil {
		t.Fatalf("new local l2 rpc: %v", err)
	}
	blockID, err := client.LastCertainBlockIDByBatchID(context.Background(), 7)
	if err != nil {
		t.Fatalf("last certain block: %v", err)
	}
	if blockID != 42 {
		t.Fatalf("unexpected block id: %d", blockID)
	}
	origin, err := client.LastCertainL1OriginByBatchID(context.Background(), 7)
	if err != nil {
		t.Fatalf("last certain l1 origin: %v", err)
	}
	if origin.BlockID != 42 || origin.L2BlockHash.Hex() != testHash("aa") {
		t.Fatalf("unexpected origin: %#v", origin)
	}
	if !origin.L1BlockHeightValid || origin.L1BlockHeight != 100 {
		t.Fatalf("unexpected l1 height: valid=%v height=%d", origin.L1BlockHeightValid, origin.L1BlockHeight)
	}
	if !origin.L1BlockHashValid || origin.L1BlockHash.Hex() != testHash("bb") {
		t.Fatalf("unexpected l1 hash: valid=%v hash=%s", origin.L1BlockHashValid, origin.L1BlockHash.Hex())
	}
	if len(methods) != 2 {
		t.Fatalf("unexpected method count: %d", len(methods))
	}
}

func shastaExtraDataHex(proposalID uint64) string {
	var proposalBytes [8]byte
	binary.BigEndian.PutUint64(proposalBytes[:], proposalID)
	return "0x00" + hex.EncodeToString(proposalBytes[2:])
}
