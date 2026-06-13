package prover

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
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

func TestTDXGethServiceDirectAggregateBuildsCarryFromLocalHeaders(t *testing.T) {
	firstCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 7,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 123 },
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			}
		}
	}`)
	secondCarry := mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 8,
			"proposal_hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			"parent_proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_block_hash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"actual_prover": "0x0000777735367b36bC9B61C50022d9D0700dB4Ec",
			"transition": { "proposer": "0x1111111111111111111111111111111111111111", "timestamp": 124 },
			"checkpoint": {
				"blockNumber": "0x2b",
				"blockHash": "0x9999999999999999999999999999999999999999999999999999999999999999",
				"stateRoot": "0x8888888888888888888888888888888888888888888888888888888888888888"
			}
		}
	}`)
	firstProposal := directProposalFromCarry(t, firstCarry, []uint64{42})
	secondProposal := directProposalFromCarry(t, secondCarry, []uint64{43})
	req := protocol.ShastaDirectAggregateRequest{
		Schema: protocol.ShastaDirectAggregateRequestSchemaV1,
		Payload: protocol.ShastaDirectAggregatePayload{
			Proposals: []protocol.DirectAggregateProposal{firstProposal, secondProposal},
		},
	}
	validated, err := ValidateDirectAggregateRequest(req)
	if err != nil {
		t.Fatalf("validate direct aggregate request: %v", err)
	}

	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			41: {
				Number:          41,
				Hash:            common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
				ProposalID:      6,
				ProposalIDValid: true,
			},
			42: {
				Number:          42,
				Hash:            common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
				ParentHash:      common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
				StateRoot:       common.HexToHash("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
				ProposalID:      7,
				ProposalIDValid: true,
			},
			43: {
				Number:          43,
				Hash:            common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
				ParentHash:      common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
				StateRoot:       common.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888"),
				ProposalID:      8,
				ProposalIDValid: true,
			},
			44: {
				Number:          44,
				Hash:            common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777"),
				ParentHash:      common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
				ProposalID:      9,
				ProposalIDValid: true,
			},
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(shastaNativeMockInstance))

	result, err := service.DirectAggregate(context.Background(), validated)
	if err != nil {
		t.Fatalf("direct aggregate: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected proof")
	}
	if !source.requested(41) || !source.requested(44) {
		t.Fatalf("expected boundary header lookups, got %v", source.requestedNumbers)
	}
	if len(result.ProofCarryDataVec) != 2 {
		t.Fatalf("unexpected carry vector length: %d", len(result.ProofCarryDataVec))
	}

	expectedInput, err := hashShastaAggregationInput(
		[]json.RawMessage{firstCarry, secondCarry},
		common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec"),
	)
	if err != nil {
		t.Fatalf("hash expected aggregation input: %v", err)
	}
	if result.Input != expectedInput.Hex() {
		t.Fatalf("unexpected input: got %s want %s", result.Input, expectedInput.Hex())
	}
}

func TestTDXGethServiceDirectAggregateRejectsBlockOutsideProposal(t *testing.T) {
	req := validatedSingleDirectAggregateRequest(t, 7, []uint64{42})
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			41: proposalHeader(41, 6, testHash("cc"), testHash("bb")),
			42: proposalHeader(42, 8, testHash("dd"), testHash("cc")),
			43: proposalHeader(43, 8, testHash("ee"), testHash("dd")),
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(shastaNativeMockInstance))

	_, err := service.DirectAggregate(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "proposal id mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTDXGethServiceDirectAggregateRejectsLeftBoundaryInsideProposal(t *testing.T) {
	req := validatedSingleDirectAggregateRequest(t, 7, []uint64{42})
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			41: proposalHeader(41, 7, testHash("cc"), testHash("bb")),
			42: proposalHeader(42, 7, testHash("dd"), testHash("cc")),
			43: proposalHeader(43, 8, testHash("ee"), testHash("dd")),
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(shastaNativeMockInstance))

	_, err := service.DirectAggregate(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "left boundary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTDXGethServiceDirectAggregateRequiresRightBoundary(t *testing.T) {
	req := validatedSingleDirectAggregateRequest(t, 7, []uint64{42})
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			41: proposalHeader(41, 6, testHash("cc"), testHash("bb")),
			42: proposalHeader(42, 7, testHash("dd"), testHash("cc")),
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(shastaNativeMockInstance))

	_, err := service.DirectAggregate(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "right boundary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTDXGethServiceDirectAggregateRejectsWrongRightBoundaryProposal(t *testing.T) {
	req := validatedSingleDirectAggregateRequest(t, 7, []uint64{42})
	source := fakeL2HeaderSource{
		headers: map[uint64]L2Header{
			41: proposalHeader(41, 6, testHash("cc"), testHash("bb")),
			42: proposalHeader(42, 7, testHash("dd"), testHash("cc")),
			43: proposalHeader(43, 7, testHash("ee"), testHash("dd")),
		},
	}
	service := NewTDXGethService(&source, NewNativeProofSigner(shastaNativeMockInstance))

	_, err := service.DirectAggregate(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "right boundary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDirectAggregateRequestRejectsBrokenProposalContinuity(t *testing.T) {
	firstCarry := sampleCarryData(t, 167013, testHash("11"), "0x2a", testHash("aa"), testHash("bb"))
	secondCarry := sampleCarryData(t, 167013, testHash("aa"), "0x2b", testHash("cc"), testHash("dd"))
	firstProposal := directProposalFromCarry(t, firstCarry, []uint64{42})
	secondProposal := directProposalFromCarry(t, secondCarry, []uint64{43})
	secondProposal.ProposalID = 9

	_, err := ValidateDirectAggregateRequest(protocol.ShastaDirectAggregateRequest{
		Schema: protocol.ShastaDirectAggregateRequestSchemaV1,
		Payload: protocol.ShastaDirectAggregatePayload{
			Proposals: []protocol.DirectAggregateProposal{firstProposal, secondProposal},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "proposal_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func validatedSingleDirectAggregateRequest(
	t *testing.T,
	proposalID uint64,
	blockNumbers []uint64,
) *ValidatedDirectAggregateRequest {
	t.Helper()

	parentHash := testHash("cc")
	blockHash := testHash("dd")
	stateRoot := testHash("ee")
	carry := sampleCarryData(t, 167013, parentHash, "0x2a", blockHash, stateRoot)
	proposal := directProposalFromCarry(t, carry, blockNumbers)
	proposal.ProposalID = proposalID
	proposal.ProposalHash = testHash("aa")
	proposal.ParentProposalHash = testHash("bb")

	validated, err := ValidateDirectAggregateRequest(protocol.ShastaDirectAggregateRequest{
		Schema: protocol.ShastaDirectAggregateRequestSchemaV1,
		Payload: protocol.ShastaDirectAggregatePayload{
			Proposals: []protocol.DirectAggregateProposal{proposal},
		},
	})
	if err != nil {
		t.Fatalf("validate direct aggregate request: %v", err)
	}
	return validated
}

func proposalHeader(number uint64, proposalID uint64, hash string, parentHash string) L2Header {
	return L2Header{
		Number:          number,
		Hash:            common.HexToHash(hash),
		ParentHash:      common.HexToHash(parentHash),
		StateRoot:       common.HexToHash("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		ProposalID:      proposalID,
		ProposalIDValid: true,
	}
}

func TestNewLocalL2RPCRejectsExternalEndpoint(t *testing.T) {
	_, err := NewLocalL2RPC("http://example.com:8545")
	if err == nil || !strings.Contains(err.Error(), "must be local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewLocalL2RPCAllowsExternalEndpointWhenExplicitlyEnabled(t *testing.T) {
	client, err := NewLocalL2RPCWithOptions("http://example.com:8545", L2RPCOptions{
		AllowRemote: true,
	})
	if err != nil {
		t.Fatalf("new l2 rpc: %v", err)
	}
	if client.endpoint != "http://example.com:8545" {
		t.Fatalf("unexpected endpoint: %s", client.endpoint)
	}
}

func directProposalFromCarry(
	t *testing.T,
	raw json.RawMessage,
	blockNumbers []uint64,
) protocol.DirectAggregateProposal {
	t.Helper()

	carry, err := decodeCarry(raw)
	if err != nil {
		t.Fatalf("decode carry: %v", err)
	}
	input := carry.TransitionInput
	return protocol.DirectAggregateProposal{
		ChainID:            carry.ChainID,
		Verifier:           carry.Verifier.Hex(),
		ProposalID:         input.ProposalID,
		ProposalHash:       input.ProposalHash.Hex(),
		ParentProposalHash: input.ParentProposalHash.Hex(),
		ActualProver:       input.ActualProver.Hex(),
		Transition: protocol.DirectAggregateTransition{
			Proposer:  input.Transition.Proposer.Hex(),
			Timestamp: input.Transition.Timestamp,
		},
		L2BlockNumbers: blockNumbers,
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
	newLocalL2HeaderSourceFn = func(rawURL string, opts L2RPCOptions) (L2HeaderSource, error) {
		if rawURL != "http://127.0.0.1:8545" {
			t.Fatalf("unexpected l2 rpc url: %s", rawURL)
		}
		if opts.AllowRemote {
			t.Fatalf("unexpected remote l2 rpc override")
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
	headers          map[uint64]L2Header
	err              error
	calls            int
	requestedNumbers []uint64
}

func (s *fakeL2HeaderSource) HeaderByNumber(_ context.Context, number uint64) (L2Header, error) {
	s.calls++
	s.requestedNumbers = append(s.requestedNumbers, number)
	if s.err != nil {
		return L2Header{}, s.err
	}
	header, ok := s.headers[number]
	if !ok {
		return L2Header{}, errors.New("missing header")
	}
	return header, nil
}

func (s *fakeL2HeaderSource) requested(number uint64) bool {
	for _, item := range s.requestedNumbers {
		if item == number {
			return true
		}
	}
	return false
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
