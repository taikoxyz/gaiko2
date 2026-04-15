package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func (f fakeService) Aggregate(context.Context, *prover.ValidatedAggregateRequest) (protocol.ProofResult, error) {
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
	req := httptest.NewRequest(
		http.MethodPost,
		"/prove/shasta",
		bytes.NewReader(loadSharedShastaRequestJSON(t)),
	)
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

func loadSharedShastaRequestJSON(t *testing.T) []byte {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	data, err := os.ReadFile(filepath.Join(root, "testdata", "shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json"))
	if err != nil {
		t.Fatalf("read shared shasta request fixture: %v", err)
	}
	return data
}
