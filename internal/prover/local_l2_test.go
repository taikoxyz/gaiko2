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
	if err == nil || err.Error() != "local L2 eth_getBlockByNumber(42) returned HTTP 502" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func shastaExtraDataHex(proposalID uint64) string {
	var proposalBytes [8]byte
	binary.BigEndian.PutUint64(proposalBytes[:], proposalID)
	return "0x00" + hex.EncodeToString(proposalBytes[2:])
}
