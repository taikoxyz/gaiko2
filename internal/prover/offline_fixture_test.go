package prover

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestOfflineProposalFixture(t *testing.T) {
	const requestEnv = "GAIKO2_OFFLINE_REGRESSION_REQUEST"
	const expectedEnv = "GAIKO2_OFFLINE_REGRESSION_EXPECTED_OUTPUT_FILE"
	requestPath, requestSet := os.LookupEnv(requestEnv)
	expectedPath, expectedSet := os.LookupEnv(expectedEnv)
	if !requestSet && !expectedSet {
		t.Skipf("set %s and %s to replay an external offline fixture", requestEnv, expectedEnv)
	}
	if strings.TrimSpace(requestPath) == "" || strings.TrimSpace(expectedPath) == "" {
		t.Fatalf("%s and %s must both name non-empty file paths", requestEnv, expectedEnv)
	}

	expectedData, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read offline fixture expected output: %v", err)
	}
	expectedText := strings.TrimSpace(string(expectedData))
	if !strings.HasPrefix(expectedText, "0x") {
		t.Fatal("offline fixture expected output must be a 0x-prefixed 32-byte hex commitment")
	}
	var expected common.Hash
	if err := expected.UnmarshalText([]byte(expectedText)); err != nil {
		t.Fatalf("decode offline fixture expected output as a 32-byte hex commitment: %v", err)
	}

	requestData, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read offline fixture request: %v", err)
	}
	var req protocol.ShastaRequest
	if err := json.Unmarshal(requestData, &req); err != nil {
		t.Fatalf("decode offline fixture request: %v", err)
	}
	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate offline fixture request: %v", err)
	}

	// Explicit configuration keeps ambient TEE settings out of this offline test.
	service, err := NewConfiguredReplayService(ServiceConfig{Mode: ProvingModeNative}, nil)
	if err != nil {
		t.Fatalf("configure native offline replay: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := service.Prove(ctx, validated)
	if err != nil {
		t.Fatalf("replay offline fixture: %v", err)
	}
	var actual common.Hash
	if err := actual.UnmarshalText([]byte(result.Input)); err != nil {
		t.Fatalf("decode offline fixture result.input as a 32-byte hex commitment: %v", err)
	}
	if actual != expected {
		t.Fatalf("offline fixture commitment mismatch: result.input=%s expected=%s", actual, expected)
	}
}
