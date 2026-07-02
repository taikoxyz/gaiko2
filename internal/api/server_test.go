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

type recordingService struct {
	proveCalls int
	result     protocol.ProofResult
	err        error
}

func (f *recordingService) Prove(context.Context, *prover.ValidatedRequest) (protocol.ProofResult, error) {
	f.proveCalls++
	return f.result, f.err
}

func (f *recordingService) Aggregate(context.Context, *prover.ValidatedAggregateRequest) (protocol.ProofResult, error) {
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

func TestNewServerRejectsV1WithoutGuestInput(t *testing.T) {
	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewBufferString(`{
		"schema":"raiko2-shasta-request-v1",
		"payload":{}
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
	if resp.Error == nil || resp.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "guest_input") {
		t.Fatalf("expected guest_input validation message, got %+v", resp.Error)
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

func TestNewServerRejectsUnsupportedProposalV2(t *testing.T) {
	service := &recordingService{}
	server := NewServer(service)
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewBufferString(`{
		"schema":"raiko2-shasta-request-v2",
		"payload":{}
	}`))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if service.proveCalls != 0 {
		t.Fatalf("legacy v1 request reached prover service %d time(s)", service.proveCalls)
	}

	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if resp.Error == nil ||
		!strings.Contains(resp.Error.Message, `unsupported schema "raiko2-shasta-request-v2"`) {
		t.Fatalf("expected unsupported v2 schema, got %+v", resp.Error)
	}
}

func TestNewServerKeepsAggregateV1EnabledWhenProposalV1Disabled(t *testing.T) {
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
	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "aggregate proof") {
		t.Fatalf("expected aggregate validator response, got %+v", resp.Error)
	}
	if resp.Error != nil && strings.Contains(resp.Error.Message, "disabled") {
		t.Fatalf("aggregate v1 should not be affected by proposal v1 policy: %+v", resp.Error)
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
	if !strings.Contains(logs.String(), "proposal_id=2222") {
		t.Fatalf("expected proposal id in success log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "chain_id=167000") {
		t.Fatalf("expected chain id in success log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "block_count=192") {
		t.Fatalf("expected block count in success log, got %q", logs.String())
	}
	if strings.Contains(logs.String(), "input_prefix=") || strings.Contains(logs.String(), "input=") {
		t.Fatalf("expected input to be omitted from success log, got %q", logs.String())
	}
}

func TestNewServerLogsGuestInputMetadataOnValidationFailure(t *testing.T) {
	var logs bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	var request map[string]any
	if err := json.Unmarshal(loadSharedShastaRequestJSON(t), &request); err != nil {
		t.Fatalf("decode shared request: %v", err)
	}
	payload := request["payload"].(map[string]any)
	guestInput := payload["guest_input"].(map[string]any)
	taiko := guestInput["taiko"].(map[string]any)
	taiko["data_sources"] = []any{}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal mutated request: %v", err)
	}

	server := NewServer(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if !strings.Contains(logs.String(), "failed prove/shasta request") {
		t.Fatalf("expected failure log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "chain_id=167000") {
		t.Fatalf("expected guest_input chain id in failure log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "block_count=192") {
		t.Fatalf("expected guest_input block count in failure log, got %q", logs.String())
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

func TestAggregateProposalIDSummary(t *testing.T) {
	proofs := []prover.AggregateProofView{
		{Carry: prover.CarryView{TransitionInput: prover.TransitionInputView{ProposalID: 7}}},
		{Carry: prover.CarryView{TransitionInput: prover.TransitionInputView{ProposalID: 8}}},
		{Carry: prover.CarryView{TransitionInput: prover.TransitionInputView{ProposalID: 9}}},
	}
	if got := aggregateProposalIDSummary(proofs); got != "7..9" {
		t.Fatalf("unexpected proposal id summary: %s", got)
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
