package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	if resp.Schema != protocol.ProofSchemaV1 {
		t.Fatalf("unexpected response schema: %s", resp.Schema)
	}
	if resp.Result == nil || resp.Result.Input != "0xinput" {
		t.Fatalf("unexpected result payload: %+v", resp.Result)
	}
}

func TestNewServerLogsProveSuccess(t *testing.T) {
	var logs bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	server := NewServer(fakeService{
		result: protocol.ProofResult{
			Input: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
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
	if !strings.Contains(logs.String(), "completed prove/shasta request") {
		t.Fatalf("expected success log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), `input_prefix="0x1234567890...`) {
		t.Fatalf("expected shortened input prefix, got %q", logs.String())
	}
	if strings.Contains(logs.String(), "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef") {
		t.Fatalf("expected full input to be omitted, got %q", logs.String())
	}
}

func TestNewServerLogsAggregateFailure(t *testing.T) {
	var logs bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta-aggregate", bytes.NewBufferString(`{
		"schema":"raiko2-shasta-aggregate-request-v1",
		"payload":{"proofs":[]}
	}`))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if !strings.Contains(logs.String(), "failed prove/shasta-aggregate request") {
		t.Fatalf("expected failure log, got %q", logs.String())
	}
}

func TestNewServerReturnsHealthzOK(t *testing.T) {
	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("unexpected response status: %s", resp.Status)
	}
}

func TestNewServerRejectsNonGetHealthz(t *testing.T) {
	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != protocol.ProofStatusError {
		t.Fatalf("unexpected response status: %s", resp.Status)
	}
	if resp.Error == nil || resp.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
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
