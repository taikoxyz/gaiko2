package prover

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type fakeRunner struct {
	stateRoot   common.Hash
	receiptRoot common.Hash
	logs        []*types.Log
	err         error
}

func (f fakeRunner) Execute(
	context.Context,
	*params.ChainConfig,
	*types.Block,
	*ReplayWitness,
) (ReplayResult, error) {
	receipts := types.Receipts(nil)
	if f.logs != nil {
		receipts = types.Receipts{&types.Receipt{Logs: f.logs}}
	}
	return ReplayResult{
		StateRoot:   f.stateRoot,
		ReceiptRoot: f.receiptRoot,
		Receipts:    receipts,
	}, f.err
}

func TestDecodeReplayBlockBuildsGethTypes(t *testing.T) {
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "0x1111111111111111111111111111111111111111111111111111111111111111",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1"
			},
			"body": {
				"transactions": [],
				"ommers": [],
				"withdrawals": null
			}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}

	block, witness, err := decodeReplayBlock(replay)
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}

	if block.NumberU64() != 42 {
		t.Fatalf("unexpected block number: %d", block.NumberU64())
	}
	if block.Root() != common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222") {
		t.Fatalf("unexpected state root: %s", block.Root())
	}
	if len(witness.Witness.Headers) != 1 {
		t.Fatalf("unexpected witness headers length: %d", len(witness.Witness.Headers))
	}
	if witness.Witness.Root() != common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("unexpected witness root: %s", witness.Witness.Root())
	}
}

func TestHashShastaSubproofInputSeparatesDomainFields(t *testing.T) {
	raw := mustRawMessage(t, `{
		"chain_id": 167000,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 42,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x1111111111111111111111111111111111111111",
			"transition": {
				"proposer": "0x2222222222222222222222222222222222222222",
				"timestamp": 123
			},
			"checkpoint": {
				"blockNumber": "0x7",
				"blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			}
		}
	}`)

	base, err := hashShastaSubproofInput(raw)
	if err != nil {
		t.Fatalf("hash base carry: %v", err)
	}

	diffChain, err := hashShastaSubproofInput(mustRawMessage(t, `{
		"chain_id": 167001,
		"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
		"transition_input": {
			"proposal_id": 42,
			"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"parent_block_hash": "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"actual_prover": "0x1111111111111111111111111111111111111111",
			"transition": {"proposer": "0x2222222222222222222222222222222222222222", "timestamp": 123},
			"checkpoint": {"blockNumber": "0x7", "blockHash": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "stateRoot": "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
		}
	}`))
	if err != nil {
		t.Fatalf("hash diff chain carry: %v", err)
	}

	if base == diffChain {
		t.Fatalf("expected domain-separated hash, got same value %s", base)
	}
}

func TestReplayServiceReturnsSignedProofResult(t *testing.T) {
	parentHash := "0x34fe3e0e24b470b507cd4547aeb65b45bf6dd1de31d3323057e2188dc37fe010"
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "`+parentHash+`",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1"
			},
			"body": {"transactions": [], "ommers": [], "withdrawals": null}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}
	blockHash := replayBlockHash(t, replay.Block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks:  []protocol.ReplayBlock{replay},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 167013,
				"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
				"transition_input": {
					"proposal_id": 42,
					"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"parent_block_hash": "`+parentHash+`",
					"actual_prover": "0x1111111111111111111111111111111111111111",
					"transition": {
						"proposer": "0x2222222222222222222222222222222222222222",
						"timestamp": 123
					},
					"checkpoint": {
						"blockNumber": "0x2a",
						"blockHash": %q,
						"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222"
					}
				}
			}`, blockHash)),
		},
	}
	validated := validatedReplayRequestForTest(t, req)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	})

	result, err := service.Prove(context.Background(), validated)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected signed proof result, got %+v", result)
	}
	if result.PublicKey == nil || *result.PublicKey == "" {
		t.Fatalf("expected public key, got %+v", result)
	}
	if result.InstanceAddress == nil || *result.InstanceAddress == "" {
		t.Fatalf("expected instance address, got %+v", result)
	}
	if result.Quote != nil {
		t.Fatalf("expected native proof result without quote, got %+v", result)
	}
	if result.Input == "" {
		t.Fatalf("expected input hash, got %+v", result)
	}
}

func TestReplayServiceRejectsValidatedRequestWithoutReplayBlocks(t *testing.T) {
	validated := sampleValidatedReplayRequestWithGuestLastAnchor(t, 167013, 900)
	validated.Request.Payload.Blocks = nil
	validated.Blocks = nil

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	})
	_, err := service.Prove(context.Background(), validated)
	if err == nil || !strings.Contains(err.Error(), "replay block") {
		t.Fatalf("expected empty replay block rejection, got %v", err)
	}
}

func TestReplayServiceSignsValidatedCarryWhenRawCarryContainsAlias(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	input := fixture.view(t).Raw
	original := string(input.ProofCarryData)
	mutated := strings.Replace(
		original,
		`"stateRoot": "0x5555555555555555555555555555555555555555555555555555555555555555"`,
		`"stateRoot": "0x5555555555555555555555555555555555555555555555555555555555555555", "STATEROOT": "0x6666666666666666666666666666666666666666666666666666666666666666"`,
		1,
	)
	if mutated == original {
		t.Fatal("failed to add alternate-case state root")
	}
	input.ProofCarryData = []byte(mutated)

	validated, err := ValidateRequest(protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			GuestInput: &input,
		},
	})
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}

	expectedInput := hashShastaSubproofCarry(validated.Carry).Hex()
	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"),
		receiptRoot: common.HexToHash(testHash("57")),
		logs: []*types.Log{
			anchorEventLog(t, fixture.chainID, 899, 900),
		},
	})
	result, err := service.Prove(context.Background(), validated)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if result.Input != expectedInput {
		t.Fatalf("signed input hash mismatch: got %s want %s", result.Input, expectedInput)
	}
}

func TestReplayServiceRejectsLastAnchorMismatchFromAnchorEvent(t *testing.T) {
	const chainID = uint64(167013)
	validated := sampleValidatedReplayRequestWithGuestLastAnchor(t, chainID, 899)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		logs: []*types.Log{
			anchorEventLog(t, chainID, 900, 901),
		},
	})
	_, err := service.Prove(context.Background(), validated)
	if err == nil || !strings.Contains(err.Error(), "last_anchor_block_number") {
		t.Fatalf("expected last anchor mismatch from replay event, got %v", err)
	}
}

func TestReplayServiceAcceptsMatchingLastAnchorFromAnchorEvent(t *testing.T) {
	const chainID = uint64(167013)
	validated := sampleValidatedReplayRequestWithGuestLastAnchor(t, chainID, 900)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		logs: []*types.Log{
			anchorEventLog(t, chainID, 900, 901),
		},
	})
	if _, err := service.Prove(context.Background(), validated); err != nil {
		t.Fatalf("prove with matching anchor event: %v", err)
	}
}

func TestReplayServiceAllowsEmptyRequestsHash(t *testing.T) {
	parentHash := "0x34fe3e0e24b470b507cd4547aeb65b45bf6dd1de31d3323057e2188dc37fe010"
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "`+parentHash+`",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1",
				"requestsHash": "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			},
			"body": {"transactions": [], "ommers": [], "withdrawals": null}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}
	blockHash := replayBlockHash(t, replay.Block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks:  []protocol.ReplayBlock{replay},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 167013,
				"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
				"transition_input": {
					"proposal_id": 42,
					"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"parent_block_hash": %q,
					"actual_prover": "0x1111111111111111111111111111111111111111",
					"transition": {
						"proposer": "0x2222222222222222222222222222222222222222",
						"timestamp": 123
					},
					"checkpoint": {
						"blockNumber": "0x2a",
						"blockHash": %q,
						"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222"
					}
				}
			}`, parentHash, blockHash)),
		},
	}
	validated := validatedReplayRequestForTest(t, req)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	})

	result, err := service.Prove(context.Background(), validated)
	if err != nil {
		t.Fatalf("prove with empty requests hash: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected signed proof result, got %+v", result)
	}
}

func TestValidateReplayRequestsHashAllowsMatchingRequests(t *testing.T) {
	requests := [][]byte{{0x01, 0xaa, 0xbb}}
	hash := types.CalcRequestsHash(requests)
	header := &types.Header{RequestsHash: &hash}

	if err := validateReplayRequestsHash(header, requests); err != nil {
		t.Fatalf("validate requests hash: %v", err)
	}
}

func TestValidateReplayRequestsHashRejectsMismatch(t *testing.T) {
	expected := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	header := &types.Header{RequestsHash: &expected}

	err := validateReplayRequestsHash(header, [][]byte{{0x01, 0xaa, 0xbb}})
	if err == nil || err.Error() == "" {
		t.Fatalf("expected requests hash mismatch error, got %v", err)
	}
}

func TestReplayServiceRejectsWitnessParentMismatch(t *testing.T) {
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "0x1111111111111111111111111111111111111111111111111111111111111111",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1"
			},
			"body": {"transactions": [], "ommers": [], "withdrawals": null}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}
	blockHash := replayBlockHash(t, replay.Block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks:  []protocol.ReplayBlock{replay},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 167013,
				"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
				"transition_input": {
					"proposal_id": 42,
					"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"parent_block_hash": "0x1111111111111111111111111111111111111111111111111111111111111111",
					"actual_prover": "0x1111111111111111111111111111111111111111",
					"transition": {
						"proposer": "0x2222222222222222222222222222222222222222",
						"timestamp": 123
					},
					"checkpoint": {
						"blockNumber": "0x2a",
						"blockHash": %q,
						"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222"
					}
				}
			}`, blockHash)),
		},
	}
	validated := validatedReplayRequestForTest(t, req)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	})
	_, err := service.Prove(context.Background(), validated)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected witness parent mismatch error, got %v", err)
	}
}

func TestReplayServiceRejectsTransactionRootMismatch(t *testing.T) {
	parentHash := "0x34fe3e0e24b470b507cd4547aeb65b45bf6dd1de31d3323057e2188dc37fe010"
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "`+parentHash+`",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x4444444444444444444444444444444444444444444444444444444444444444",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1"
			},
			"body": {"transactions": [], "ommers": [], "withdrawals": null}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}
	blockHash := replayBlockHash(t, replay.Block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: 167013,
			Blocks:  []protocol.ReplayBlock{replay},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": 167013,
				"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
				"transition_input": {
					"proposal_id": 42,
					"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"parent_block_hash": %q,
					"actual_prover": "0x1111111111111111111111111111111111111111",
					"transition": {
						"proposer": "0x2222222222222222222222222222222222222222",
						"timestamp": 123
					},
					"checkpoint": {
						"blockNumber": "0x2a",
						"blockHash": %q,
						"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222"
					}
				}
			}`, parentHash, blockHash)),
		},
	}
	validated := validatedReplayRequestForTest(t, req)

	service := NewReplayService(fakeRunner{
		stateRoot:   common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		receiptRoot: common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	})
	_, err := service.Prove(context.Background(), validated)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected transaction root mismatch error, got %v", err)
	}
}

func TestChainConfigForMasayaEnablesBlobForkAtUnzen(t *testing.T) {
	const unzenTime uint64 = 0

	cfg, err := chainConfigFor(params.MasayaDevnetNetworkID.Uint64())
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	if cfg.UnzenTime == nil || *cfg.UnzenTime != unzenTime {
		t.Fatalf("unexpected unzen time: %v", cfg.UnzenTime)
	}
	if cfg.CancunTime == nil || *cfg.CancunTime != unzenTime {
		t.Fatalf("unexpected cancun time: %v", cfg.CancunTime)
	}
	if cfg.PragueTime == nil || *cfg.PragueTime != unzenTime {
		t.Fatalf("unexpected prague time: %v", cfg.PragueTime)
	}
	if cfg.OsakaTime == nil || *cfg.OsakaTime != unzenTime {
		t.Fatalf("unexpected osaka time: %v", cfg.OsakaTime)
	}
	if cfg.BlobScheduleConfig == nil || cfg.BlobScheduleConfig.Cancun == nil || cfg.BlobScheduleConfig.Prague == nil || cfg.BlobScheduleConfig.Osaka == nil {
		t.Fatalf("missing blob schedule config: %+v", cfg.BlobScheduleConfig)
	}
	if !cfg.IsCancun(common.Big0, unzenTime) {
		t.Fatalf("expected cancun activation at unzen")
	}
	if !cfg.IsUnzen(unzenTime) {
		t.Fatalf("expected unzen activation at unzen")
	}
}

func validatedReplayRequestForTest(t *testing.T, req protocol.ShastaRequest) *ValidatedRequest {
	t.Helper()

	carry, err := decodeCarry(req.Payload.ProofCarryData)
	if err != nil {
		t.Fatalf("decode carry: %v", err)
	}

	blocks := make([]BlockView, 0, len(req.Payload.Blocks))
	for index, block := range req.Payload.Blocks {
		view, err := decodeBlock(block)
		if err != nil {
			t.Fatalf("decode replay block %d: %v", index, err)
		}
		blocks = append(blocks, view)
	}

	return &ValidatedRequest{
		Request: req,
		Carry:   carry,
		Blocks:  blocks,
	}
}

func sampleValidatedReplayRequestWithGuestLastAnchor(
	t *testing.T,
	chainID uint64,
	lastAnchor uint64,
) *ValidatedRequest {
	t.Helper()

	parentHash := "0x34fe3e0e24b470b507cd4547aeb65b45bf6dd1de31d3323057e2188dc37fe010"
	replay := protocol.ReplayBlock{
		Block: mustRawMessage(t, `{
			"header": {
				"parentHash": "`+parentHash+`",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"miner": "0x0000000000000000000000000000000000000000",
				"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"receiptsRoot": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"difficulty": "0x0",
				"number": "0x2a",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"timestamp": "0x0",
				"extraData": "0x",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"baseFeePerGas": "0x1"
			},
			"body": {"transactions": [], "ommers": [], "withdrawals": null}
		}`),
		Witness: mustRawMessage(t, `{
			"headers": [{
				"hash": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"header": {
					"parentHash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
					"miner": "0x0000000000000000000000000000000000000000",
					"stateRoot": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
					"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
					"difficulty": "0x0",
					"number": "0x29",
					"gasLimit": "0x0",
					"gasUsed": "0x0",
					"timestamp": "0x0",
					"extraData": "0x",
					"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
					"nonce": "0x0000000000000000",
					"baseFeePerGas": "0x1"
				}
			}],
			"codes": [],
			"state": [],
			"keys": []
		}`),
	}
	blockHash := replayBlockHash(t, replay.Block)
	req := protocol.ShastaRequest{
		Schema: protocol.ShastaRequestSchemaV1,
		Payload: protocol.ShastaPayload{
			ChainID: chainID,
			Blocks:  []protocol.ReplayBlock{replay},
			ProofCarryData: mustRawMessage(t, fmt.Sprintf(`{
				"chain_id": %d,
				"verifier": "0x00f9f60C79e38c08b785eE4F1a849900693C6630",
				"transition_input": {
					"proposal_id": 42,
					"proposal_hash": "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"parent_proposal_hash": "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"parent_block_hash": %q,
					"actual_prover": "0x1111111111111111111111111111111111111111",
					"transition": {
						"proposer": "0x2222222222222222222222222222222222222222",
						"timestamp": 123
					},
					"checkpoint": {
						"blockNumber": "0x2a",
						"blockHash": %q,
						"stateRoot": "0x2222222222222222222222222222222222222222222222222222222222222222"
					}
				}
			}`, chainID, parentHash, blockHash)),
			GuestInput: &protocol.ShastaGuestInput{
				Taiko: mustRawMessage(t, fmt.Sprintf(`{
					"prover_data": {
						"last_anchor_block_number": %d
					}
				}`, lastAnchor)),
			},
		},
	}
	return validatedReplayRequestForTest(t, req)
}

func anchorEventLog(t *testing.T, chainID uint64, prevAnchor uint64, anchor uint64) *types.Log {
	t.Helper()

	data := make([]byte, 96)
	new(big.Int).SetUint64(prevAnchor).FillBytes(data[:32])
	new(big.Int).SetUint64(anchor).FillBytes(data[32:64])
	return &types.Log{
		Address: testTaikoL2Address(chainID),
		Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("Anchored(uint48,uint48,bytes32)")),
		},
		Data: data,
	}
}

func TestChainConfigForHoodiEnablesBlobForkAtUnzen(t *testing.T) {
	const unzenTime uint64 = 1781787600

	cfg, err := chainConfigFor(params.TaikoHoodiNetworkID.Uint64())
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	if cfg.UnzenTime == nil || *cfg.UnzenTime != unzenTime {
		t.Fatalf("unexpected unzen time: %v", cfg.UnzenTime)
	}
	if cfg.CancunTime == nil || *cfg.CancunTime != unzenTime {
		t.Fatalf("unexpected cancun time: %v", cfg.CancunTime)
	}
	if cfg.PragueTime == nil || *cfg.PragueTime != unzenTime {
		t.Fatalf("unexpected prague time: %v", cfg.PragueTime)
	}
	if cfg.OsakaTime == nil || *cfg.OsakaTime != unzenTime {
		t.Fatalf("unexpected osaka time: %v", cfg.OsakaTime)
	}
	if cfg.BlobScheduleConfig == nil || cfg.BlobScheduleConfig.Cancun == nil || cfg.BlobScheduleConfig.Prague == nil || cfg.BlobScheduleConfig.Osaka == nil {
		t.Fatalf("missing blob schedule config: %+v", cfg.BlobScheduleConfig)
	}
	if cfg.IsCancun(common.Big0, unzenTime-1) {
		t.Fatalf("unexpected cancun activation before unzen")
	}
	if !cfg.IsCancun(common.Big0, unzenTime) {
		t.Fatalf("expected cancun activation at unzen")
	}
	if cfg.IsUnzen(unzenTime - 1) {
		t.Fatalf("unexpected unzen activation before unzen")
	}
	if !cfg.IsUnzen(unzenTime) {
		t.Fatalf("expected unzen activation at unzen")
	}
}

func TestChainConfigForMainnetEnablesBlobForkAtUnzen(t *testing.T) {
	const unzenTime uint64 = 1786021200

	cfg, err := chainConfigFor(params.TaikoMainnetNetworkID.Uint64())
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	if cfg.UnzenTime == nil || *cfg.UnzenTime != unzenTime {
		t.Fatalf("unexpected unzen time: %v", cfg.UnzenTime)
	}
	if cfg.CancunTime == nil || *cfg.CancunTime != unzenTime {
		t.Fatalf("unexpected cancun time: %v", cfg.CancunTime)
	}
	if cfg.PragueTime == nil || *cfg.PragueTime != unzenTime {
		t.Fatalf("unexpected prague time: %v", cfg.PragueTime)
	}
	if cfg.OsakaTime == nil || *cfg.OsakaTime != unzenTime {
		t.Fatalf("unexpected osaka time: %v", cfg.OsakaTime)
	}
	if cfg.IsCancun(common.Big0, unzenTime-1) {
		t.Fatalf("unexpected cancun activation before unzen")
	}
	if !cfg.IsCancun(common.Big0, unzenTime) {
		t.Fatalf("expected cancun activation at unzen")
	}
	if cfg.IsUnzen(unzenTime - 1) {
		t.Fatalf("unexpected unzen activation before unzen")
	}
	if !cfg.IsUnzen(unzenTime) {
		t.Fatalf("expected unzen activation at unzen")
	}
}

func TestUnzenZkGasScheduleForFollowsTaikoGethDefaultSchedule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chainID uint64
	}{
		{name: "masaya", chainID: params.MasayaDevnetNetworkID.Uint64()},
		{name: "hoodi", chainID: params.TaikoHoodiNetworkID.Uint64()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := chainConfigFor(tc.chainID)
			if err != nil {
				t.Fatalf("chain config: %v", err)
			}

			schedule := unzenZkGasScheduleFor(cfg)
			if schedule != &vm.UnzenZkGasSchedule {
				t.Fatalf("unexpected unzen zk-gas schedule: %p", schedule)
			}
			if schedule.BlockLimit != vm.BlockZkGasLimit {
				t.Fatalf("unexpected block zk-gas limit: %d", schedule.BlockLimit)
			}
			if schedule.TxIntrinsicZkGas != vm.TxIntrinsicZkGas {
				t.Fatalf("unexpected tx intrinsic zk gas: %d", schedule.TxIntrinsicZkGas)
			}
		})
	}
}

func TestChainConfigForInternalDevnetUsesGenesisForks(t *testing.T) {
	cfg, err := chainConfigFor(params.TaikoInternalNetworkID.Uint64())
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	if cfg.OntakeBlock == nil || cfg.OntakeBlock.Sign() != 0 {
		t.Fatalf("unexpected ontake block: %v", cfg.OntakeBlock)
	}
	if cfg.PacayaBlock == nil || cfg.PacayaBlock.Sign() != 0 {
		t.Fatalf("unexpected pacaya block: %v", cfg.PacayaBlock)
	}
	if cfg.ShastaTime == nil || *cfg.ShastaTime != 0 {
		t.Fatalf("unexpected shasta time: %v", cfg.ShastaTime)
	}
	if cfg.UnzenTime == nil || *cfg.UnzenTime != 0 {
		t.Fatalf("unexpected unzen time: %v", cfg.UnzenTime)
	}
	if cfg.CancunTime == nil || *cfg.CancunTime != 0 {
		t.Fatalf("unexpected cancun time: %v", cfg.CancunTime)
	}
	if cfg.PragueTime == nil || *cfg.PragueTime != 0 {
		t.Fatalf("unexpected prague time: %v", cfg.PragueTime)
	}
	if cfg.OsakaTime == nil || *cfg.OsakaTime != 0 {
		t.Fatalf("unexpected osaka time: %v", cfg.OsakaTime)
	}
}

func TestReplayExecutionBlockZeroesDifficultyForUnzen(t *testing.T) {
	cfg, err := chainConfigFor(params.MasayaDevnetNetworkID.Uint64())
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	originalBlock := types.NewBlockWithHeader(&types.Header{
		Number:     big.NewInt(4140811),
		Time:       core.MasayaUnzenTime,
		Difficulty: big.NewInt(1236639),
	})
	executionBlock, expectedDifficulty := replayExecutionBlock(cfg, originalBlock)
	if executionBlock.Difficulty().Sign() != 0 {
		t.Fatalf("expected zero execution difficulty at Unzen, got %s", executionBlock.Difficulty())
	}
	if expectedDifficulty == nil || expectedDifficulty.Uint64() != 1236639 {
		t.Fatalf("expected preserved imported difficulty, got %v", expectedDifficulty)
	}
	if originalBlock.Difficulty().Uint64() != 1236639 {
		t.Fatalf("expected original difficulty to remain unchanged, got %s", originalBlock.Difficulty())
	}

	hoodiConfig, err := chainConfigFor(params.TaikoHoodiNetworkID.Uint64())
	if err != nil {
		t.Fatalf("hoodi chain config: %v", err)
	}
	preUnzenBlock := types.NewBlockWithHeader(&types.Header{
		Number:     big.NewInt(4140810),
		Time:       core.HoodiUnzenTime - 1,
		Difficulty: big.NewInt(7),
	})
	preExecutionBlock, preExpectedDifficulty := replayExecutionBlock(hoodiConfig, preUnzenBlock)
	if preExpectedDifficulty != nil {
		t.Fatalf("expected no separate imported difficulty before Unzen, got %v", preExpectedDifficulty)
	}
	if preExecutionBlock.Difficulty().Uint64() != 7 {
		t.Fatalf("expected pre-Unzen difficulty to be preserved, got %s", preExecutionBlock.Difficulty())
	}
}

func mustRawMessage(t *testing.T, value string) json.RawMessage {
	t.Helper()
	return json.RawMessage(value)
}
