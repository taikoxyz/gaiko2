package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestNewServerRejectsOversizedProveBody(t *testing.T) {
	server := newServer(fakeService{}, 64)
	body := append([]byte(`{"schema":"`), bytes.Repeat([]byte("a"), 128)...)
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "REQUEST_TOO_LARGE" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
}

func TestNewServerRejectsOversizedAggregateBody(t *testing.T) {
	server := newServer(fakeService{}, 64)
	body := append([]byte(`{"schema":"`), bytes.Repeat([]byte("a"), 128)...)
	req := httptest.NewRequest(http.MethodPost, "/prove/shasta-aggregate", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "REQUEST_TOO_LARGE" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
}

func TestNewServerRejectsOversizedValidJSONPrefix(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "prove", path: proveShastaPath},
		{name: "aggregate", path: proveShastaAggregatePath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, chunked := range []bool{false, true} {
				t.Run(fmt.Sprintf("chunked=%v", chunked), func(t *testing.T) {
					body := []byte("{}" + strings.Repeat(" ", 64))
					req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
					if chunked {
						req.ContentLength = -1
					}
					recorder := httptest.NewRecorder()
					newServer(fakeService{}, 8).ServeHTTP(recorder, req)
					if recorder.Code != http.StatusRequestEntityTooLarge {
						t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
					}
					assertProofErrorCode(t, recorder.Body.Bytes(), "REQUEST_TOO_LARGE")
				})
			}
		})
	}
}

func assertProofErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var resp protocol.ProofResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != want {
		t.Fatalf("error=%+v want code %s", resp.Error, want)
	}
}

func TestNewServerRejectsSecondJSONValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, proveShastaPath, strings.NewReader("{} {}"))
	recorder := httptest.NewRecorder()
	newServer(fakeService{}, 64).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
	assertProofErrorCode(t, recorder.Body.Bytes(), "INVALID_JSON")
}

func TestDecodeRequestAcceptsTrailingWhitespaceWithinLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, proveShastaPath, strings.NewReader("{}   \n"))
	recorder := httptest.NewRecorder()
	var dst map[string]any
	if err := decodeRequest(recorder, req, 64, &dst); err != nil {
		t.Fatalf("decode bounded request: %v", err)
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

func TestNewServerStopsValidationOnCanceledContext(t *testing.T) {
	service := &recordingService{
		result: protocol.ProofResult{Input: "0xinput"},
	}
	server := NewServer(service)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(
		http.MethodPost,
		"/prove/shasta",
		bytes.NewReader(loadSharedShastaRequestJSON(t)),
	).WithContext(ctx)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if service.proveCalls != 0 {
		t.Fatalf("canceled request reached prover service %d time(s)", service.proveCalls)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var resp protocol.ProofResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, context.Canceled.Error()) {
		t.Fatalf("expected context cancellation response, got %+v", resp.Error)
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
