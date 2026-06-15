package prover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewLocalProposalAPIRejectsExternalEndpoint(t *testing.T) {
	_, err := NewLocalProposalAPI("http://example.com:9876")
	if err == nil || !strings.Contains(err.Error(), "must be local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalProposalAPIProposalMetadataByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/internal/shasta/proposals/7" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"proposal": {
				"id": 7,
				"timestamp": 123,
				"end_of_submission_window_timestamp": 456,
				"proposer": "0x1111111111111111111111111111111111111111",
				"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"origin_block_number": 100,
				"origin_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"basefee_sharing_pctg": 0,
				"sources": []
			},
			"event": {
				"block_number": 101,
				"block_hash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"tx_hash": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				"log_index": 0
			}
		}`))
	}))
	defer server.Close()

	client, err := NewLocalProposalAPI(server.URL)
	if err != nil {
		t.Fatalf("new proposal api: %v", err)
	}
	metadata, err := client.ProposalMetadataByID(context.Background(), 7)
	if err != nil {
		t.Fatalf("proposal metadata: %v", err)
	}
	if metadata.ProposalID != 7 {
		t.Fatalf("unexpected proposal id: %d", metadata.ProposalID)
	}
	if metadata.ProposalHash.Hex() != testHash("aa") {
		t.Fatalf("unexpected proposal hash: %s", metadata.ProposalHash.Hex())
	}
	if metadata.ParentProposalHash.Hex() != testHash("bb") {
		t.Fatalf("unexpected parent proposal hash: %s", metadata.ParentProposalHash.Hex())
	}
	if metadata.Proposer.Hex() != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected proposer: %s", metadata.Proposer.Hex())
	}
	if metadata.Timestamp != 123 {
		t.Fatalf("unexpected timestamp: %d", metadata.Timestamp)
	}
}
