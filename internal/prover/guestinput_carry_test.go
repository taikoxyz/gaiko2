package prover

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

const (
	shastaProposalKnownVectorHash = "0x13af2d05799894db3462512e3ecf5ae8877b80b1e2db3963654ac70f6dd49f88"
	shastaProposalKnownVectorJSON = `{
		"id": 12345,
		"timestamp": 193828690,
		"endOfSubmissionWindowTimestamp": 193829690,
		"proposer": "0x1234567890abcdef1234567890abcdef12345678",
		"parentProposalHash": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"originBlockNumber": 73826,
		"originBlockHash": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"basefeeSharingPctg": 42,
		"sources": [
			{
				"isForcedInclusion": true,
				"blobSlice": {
					"blobHashes": ["0x67890abcdef1234567890abcdef123451234567890abcdef1234567890abcdef"],
					"offset": 0,
					"timestamp": 100
				}
			},
			{
				"isForcedInclusion": false,
				"blobSlice": {
					"blobHashes": ["0x567890abcdef123451234567890abcdef123456767890abcdef1234890abcdef"],
					"offset": 100,
					"timestamp": 200
				}
			}
		]
	}`
)

func TestGuestInputCarryPassesForDerivedValues(t *testing.T) {
	view := decodeGuestInputCarryView(t, newGuestInputCarryFixture(t))

	if err := ValidateGuestInputCarry(view); err != nil {
		t.Fatalf("validate guest input carry: %v", err)
	}
}

func TestGuestInputCarryRejectsMismatches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*guestInputCarryFixture)
		wantErr string
	}{
		{
			name: "chain_id",
			mutate: func(f *guestInputCarryFixture) {
				f.chainID++
			},
			wantErr: "chain_id mismatch",
		},
		{
			name: "proposal_id",
			mutate: func(f *guestInputCarryFixture) {
				f.proposalID++
			},
			wantErr: "proposal_id mismatch",
		},
		{
			name: "proposal_hash",
			mutate: func(f *guestInputCarryFixture) {
				f.proposalHash = testHash("99")
			},
			wantErr: "proposal_hash mismatch",
		},
		{
			name: "parent_proposal_hash",
			mutate: func(f *guestInputCarryFixture) {
				f.parentProposalHash = testHash("98")
			},
			wantErr: "parent_proposal_hash mismatch",
		},
		{
			name: "parent_block_hash",
			mutate: func(f *guestInputCarryFixture) {
				f.parentBlockHash = testHash("97")
			},
			wantErr: "parent_block_hash mismatch",
		},
		{
			name: "actual_prover",
			mutate: func(f *guestInputCarryFixture) {
				f.actualProver = testAddress("96")
			},
			wantErr: "actual_prover mismatch",
		},
		{
			name: "proposer",
			mutate: func(f *guestInputCarryFixture) {
				f.proposer = testAddress("95")
			},
			wantErr: "transition.proposer mismatch",
		},
		{
			name: "timestamp",
			mutate: func(f *guestInputCarryFixture) {
				f.timestamp++
			},
			wantErr: "transition.timestamp mismatch",
		},
		{
			name: "checkpoint block number",
			mutate: func(f *guestInputCarryFixture) {
				f.checkpointNumber = "0x2b"
			},
			wantErr: "checkpoint.blockNumber mismatch",
		},
		{
			name: "checkpoint block hash",
			mutate: func(f *guestInputCarryFixture) {
				f.checkpointBlockHash = testHash("94")
			},
			wantErr: "checkpoint.blockHash mismatch",
		},
		{
			name: "checkpoint state root",
			mutate: func(f *guestInputCarryFixture) {
				f.checkpointStateRoot = testHash("93")
			},
			wantErr: "checkpoint.stateRoot mismatch",
		},
		{
			name: "verifier",
			mutate: func(f *guestInputCarryFixture) {
				f.verifier = testAddress("92")
			},
			wantErr: "verifier mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newGuestInputCarryFixture(t)
			tc.mutate(fixture)
			view := decodeGuestInputCarryView(t, fixture)

			err := ValidateGuestInputCarry(view)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGuestInputCarryRejectsMissingGuestInputChainID(t *testing.T) {
	fixture := newGuestInputCarryFixture(t)
	fixture.witnessChainSpec = mustRawMessage(t, fmt.Sprintf(`{
		"verifier_address_forks": {
			"SHASTA": {
				"SGXGETH": %q
			}
		}
	}`, fixture.verifier))
	fixture.taiko = mustRawMessage(t, fmt.Sprintf(`{
		"proposal_id": 12345,
		"proposal_event": {
			"proposal": %s
		},
		"prover_data": {
			"actual_prover": %q
		}
	}`, shastaProposalKnownVectorJSON, fixture.actualProver))
	view := decodeGuestInputCarryView(t, fixture)

	err := ValidateGuestInputCarry(view)
	if err == nil || !strings.Contains(err.Error(), "guest input chain id is missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuestInputCarryRejectsMissingVerifier(t *testing.T) {
	fixture := newGuestInputCarryFixture(t)
	fixture.witnessChainSpec = mustRawMessage(t, `{
		"chain_id": 167013,
		"verifier_address_forks": {
			"SHASTA": {
				"SP1": "0x1111111111111111111111111111111111111111"
			}
		}
	}`)
	view := decodeGuestInputCarryView(t, fixture)

	err := ValidateGuestInputCarry(view)
	if err == nil || !strings.Contains(err.Error(), "missing verifier") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHashShastaProposalKnownVector(t *testing.T) {
	proposal, err := decodeShastaProposal(mustRawMessage(t, shastaProposalKnownVectorJSON))
	if err != nil {
		t.Fatalf("decode proposal: %v", err)
	}

	got, err := hashShastaProposal(proposal)
	if err != nil {
		t.Fatalf("hash proposal: %v", err)
	}
	if got != common.HexToHash(shastaProposalKnownVectorHash) {
		t.Fatalf("unexpected proposal hash: got %s want %s", got, shastaProposalKnownVectorHash)
	}
}

type guestInputCarryFixture struct {
	chainID             uint64
	verifier            string
	proposalID          uint64
	proposalHash        string
	parentProposalHash  string
	parentBlockHash     string
	actualProver        string
	proposer            string
	timestamp           uint64
	checkpointNumber    string
	checkpointBlockHash string
	checkpointStateRoot string
	block               []byte
	witnessChainSpec    json.RawMessage
	taiko               json.RawMessage
}

func newGuestInputCarryFixture(t *testing.T) *guestInputCarryFixture {
	t.Helper()

	parentBlockHash := testHash("44")
	stateRoot := testHash("22")
	blockRaw := sampleReplayBlock(t, "0x2a", parentBlockHash, stateRoot, testHash("33"))
	blockHash := replayBlockHash(t, blockRaw)
	verifier := testAddress("f9")
	actualProver := testAddress("77")

	return &guestInputCarryFixture{
		chainID:             167013,
		verifier:            verifier,
		proposalID:          12345,
		proposalHash:        shastaProposalKnownVectorHash,
		parentProposalHash:  "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		parentBlockHash:     parentBlockHash,
		actualProver:        actualProver,
		proposer:            "0x1234567890abcdef1234567890abcdef12345678",
		timestamp:           193828690,
		checkpointNumber:    "0x2a",
		checkpointBlockHash: blockHash,
		checkpointStateRoot: stateRoot,
		block:               blockRaw,
		witnessChainSpec: mustRawMessage(t, fmt.Sprintf(`{
			"chain_id": 167013,
			"verifier_address_forks": {
				"SHASTA": {
					"SGXGETH": %q
				}
			}
		}`, verifier)),
		taiko: mustRawMessage(t, fmt.Sprintf(`{
			"chain_spec": {
				"chain_id": 167013
			},
			"proposal_id": 12345,
			"proposal_event": {
				"proposal": %s
			},
			"prover_data": {
				"actual_prover": %q
			}
		}`, shastaProposalKnownVectorJSON, actualProver)),
	}
}

func decodeGuestInputCarryView(t *testing.T, fixture *guestInputCarryFixture) *GuestInputView {
	t.Helper()

	view, err := DecodeGuestInput(fixture.input(t))
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}
	return view
}

func (f *guestInputCarryFixture) input(t *testing.T) protocol.ShastaGuestInput {
	t.Helper()

	return protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": %s,
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": []},
				"accounts": {}
			}`, f.block, f.witnessChainSpec)),
		},
		Taiko:          f.taiko,
		ProofCarryData: f.proofCarryData(t),
	}
}

func (f *guestInputCarryFixture) proofCarryData(t *testing.T) json.RawMessage {
	t.Helper()

	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"verifier": %q,
		"transition_input": {
			"proposal_id": %d,
			"proposal_hash": %q,
			"parent_proposal_hash": %q,
			"parent_block_hash": %q,
			"actual_prover": %q,
			"transition": {
				"proposer": %q,
				"timestamp": %d
			},
			"checkpoint": {
				"blockNumber": %q,
				"blockHash": %q,
				"stateRoot": %q
			}
		}
	}`, f.chainID, f.verifier, f.proposalID, f.proposalHash, f.parentProposalHash, f.parentBlockHash, f.actualProver, f.proposer, f.timestamp, f.checkpointNumber, f.checkpointBlockHash, f.checkpointStateRoot))
}
