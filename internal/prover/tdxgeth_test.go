package prover

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

func TestTDXGethServiceVerifiesLocalHeadersInsteadOfReplayingWitness(t *testing.T) {
	req := fixtureValidatedRequest(t)
	signer := NewNativeProofSigner(0x12345678)
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			req.Blocks[0].Number: {
				Number:       req.Blocks[0].Number,
				Hash:         req.Blocks[0].Hash,
				ParentHash:   req.Blocks[0].ParentHash,
				StateRoot:    req.Blocks[0].StateRoot,
				ReceiptsRoot: req.Blocks[0].ReceiptsRoot,
			},
		},
	}
	service := NewTDXGethService(&source, signer)

	result, err := service.Prove(context.Background(), req)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected proof")
	}
	if source.calls != 1 {
		t.Fatalf("expected one local header lookup, got %d", source.calls)
	}
}

func fixtureValidatedRequest(t *testing.T) *ValidatedRequest {
	t.Helper()

	parentHash := testHash("11")
	block := sampleReplayBlock(t, "0x2a", parentHash, testHash("aa"), testHash("de"))
	blockHash := replayBlockHash(t, block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks: []protocol.ReplayBlock{
				{Block: block},
			},
			ProofCarryData: sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, testHash("aa")),
		},
	}
	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate fixture request: %v", err)
	}
	return validated
}

func TestTDXGethServiceRejectsLocalHeaderMismatch(t *testing.T) {
	req := fixtureValidatedRequest(t)
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			req.Blocks[0].Number: {
				Number:       req.Blocks[0].Number,
				Hash:         common.HexToHash("0x1234"),
				ParentHash:   req.Blocks[0].ParentHash,
				StateRoot:    req.Blocks[0].StateRoot,
				ReceiptsRoot: req.Blocks[0].ReceiptsRoot,
			},
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(0x12345678))

	_, err := service.Prove(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "local L2 block hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewLocalL2RPCRejectsExternalEndpoint(t *testing.T) {
	_, err := NewLocalL2RPC("http://example.com:8545")
	if err == nil || !strings.Contains(err.Error(), "must be local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredServiceSelectsTDXGethMode(t *testing.T) {
	prevProvider := newTEEProviderFn
	prevHeaderSource := newLocalL2HeaderSourceFn
	t.Cleanup(func() {
		newTEEProviderFn = prevProvider
		newLocalL2HeaderSourceFn = prevHeaderSource
	})

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	newTEEProviderFn = func(cfg tee.Config) (tee.Provider, error) {
		if cfg.Type != tee.TypeTDX {
			t.Fatalf("unexpected tee type: %s", cfg.Type)
		}
		if cfg.TDXSocket != "/var/tdxs.sock" {
			t.Fatalf("unexpected tdx socket: %s", cfg.TDXSocket)
		}
		return &configuredFakeProvider{privateKey: privateKey}, nil
	}
	newLocalL2HeaderSourceFn = func(rawURL string) (L2HeaderSource, error) {
		if rawURL != "http://127.0.0.1:8545" {
			t.Fatalf("unexpected l2 rpc url: %s", rawURL)
		}
		return &fakeL2HeaderSource{}, nil
	}

	service, err := NewConfiguredService(ServiceConfig{
		Mode:       ProvingModeTDXGeth,
		SecretDir:  t.TempDir(),
		TDXSocket:  "/var/tdxs.sock",
		L2RPCURL:   "http://127.0.0.1:8545",
		InstanceID: 0x12345678,
	}, nil)
	if err != nil {
		t.Fatalf("new configured service: %v", err)
	}
	if _, ok := service.(TDXGethService); !ok {
		t.Fatalf("expected TDXGethService, got %T", service)
	}
}

type fakeL2HeaderSource struct {
	headers map[uint64]L2Header
	err     error
	calls   int
}

func (s *fakeL2HeaderSource) HeaderByNumber(_ context.Context, number uint64) (L2Header, error) {
	s.calls++
	if s.err != nil {
		return L2Header{}, s.err
	}
	header, ok := s.headers[number]
	if !ok {
		return L2Header{}, errors.New("missing header")
	}
	return header, nil
}

type configuredFakeProvider struct {
	privateKey *ecdsa.PrivateKey
}

func (p *configuredFakeProvider) LoadQuote(common.Address) (tee.Quote, error) {
	return tee.StaticQuote([]byte{0x01}), nil
}

func (p *configuredFakeProvider) LoadQuoteForReportData([]byte) (tee.Quote, error) {
	return tee.StaticQuote([]byte{0x02}), nil
}

func (p *configuredFakeProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	return p.privateKey, nil
}

func (p *configuredFakeProvider) SavePrivateKey(*ecdsa.PrivateKey) error {
	return nil
}
