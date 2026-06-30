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
		"hard_forks": {
			"SHASTA": {"Block": 0}
		},
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

func TestGuestInputCarryAcceptsSgxVerifierSpellings(t *testing.T) {
	cases := []string{"Sgx", "SGX", "sgx", "SgxGeth", "SGXGETH", "sgxgeth", "sgx_geth"}

	for _, verifierKey := range cases {
		t.Run(verifierKey, func(t *testing.T) {
			fixture := newGuestInputCarryFixture(t)
			fixture.witnessChainSpec = guestInputChainSpec(t,
				`{"SHASTA": {"Block": 0}}`,
				fmt.Sprintf(`{"SHASTA": {%q: %q}}`, verifierKey, fixture.verifier),
			)
			view := decodeGuestInputCarryView(t, fixture)

			if err := ValidateGuestInputCarry(view); err != nil {
				t.Fatalf("validate guest input carry: %v", err)
			}
		})
	}
}

func TestGuestInputCarryRejectsMissingSgxVerifierLane(t *testing.T) {
	fixture := newGuestInputCarryFixture(t)
	fixture.witnessChainSpec = guestInputChainSpec(t,
		`{"SHASTA": {"Block": 0}}`,
		`{"SHASTA": {"Sp1": "0x1111111111111111111111111111111111111111"}}`,
	)
	view := decodeGuestInputCarryView(t, fixture)

	err := ValidateGuestInputCarry(view)
	if err == nil || !strings.Contains(err.Error(), "missing verifier") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuestInputCarrySelectsActiveVerifierFork(t *testing.T) {
	shastaVerifier := testAddress("a1")
	unzenVerifier := testAddress("b2")

	cases := []struct {
		name          string
		blockNumber   uint64
		blockTime     uint64
		verifierForks string
		wantVerifier  string
	}{
		{
			name:        "shasta active",
			blockNumber: 150,
			blockTime:   250,
			verifierForks: fmt.Sprintf(`{
				"PACAYA": {"SgxGeth": %q},
				"SHASTA": {"SgxGeth": %q},
				"UNZEN": {"SgxGeth": %q}
			}`, testAddress("c3"), shastaVerifier, unzenVerifier),
			wantVerifier: shastaVerifier,
		},
		{
			name:        "unzen active with unzen verifier",
			blockNumber: 150,
			blockTime:   600,
			verifierForks: fmt.Sprintf(`{
				"SHASTA": {"SgxGeth": %q},
				"UNZEN": {"SgxGeth": %q}
			}`, shastaVerifier, unzenVerifier),
			wantVerifier: unzenVerifier,
		},
		{
			name:        "unzen active falls back to shasta verifier",
			blockNumber: 150,
			blockTime:   600,
			verifierForks: fmt.Sprintf(`{
				"SHASTA": {"SgxGeth": %q}
			}`, shastaVerifier),
			wantVerifier: shastaVerifier,
		},
		{
			name:        "unzen active skips unzen without sgxgeth verifier",
			blockNumber: 150,
			blockTime:   600,
			verifierForks: fmt.Sprintf(`{
				"SHASTA": {"SgxGeth": %q},
				"UNZEN": {"SP1": %q}
			}`, shastaVerifier, unzenVerifier),
			wantVerifier: shastaVerifier,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newGuestInputCarryFixture(t)
			fixture.verifier = tc.wantVerifier
			setGuestInputCarryFixtureBlock(t, fixture, tc.blockNumber, tc.blockTime)
			fixture.witnessChainSpec = guestInputChainSpec(t,
				`{
					"CANCUN": "Tbd",
					"PACAYA": {"Block": 100},
					"SHASTA": {"Timestamp": 200},
					"UNZEN": {"Timestamp": 500}
				}`,
				tc.verifierForks,
			)
			view := decodeGuestInputCarryView(t, fixture)

			if err := ValidateGuestInputCarry(view); err != nil {
				t.Fatalf("validate guest input carry: %v", err)
			}
		})
	}
}

func TestGuestInputCarryRejectsMalformedActiveVerifierInsteadOfFallback(t *testing.T) {
	shastaVerifier := testAddress("a1")
	cases := []struct {
		name         string
		unzenSgxGeth string
	}{
		{name: "null", unzenSgxGeth: "null"},
		{name: "malformed", unzenSgxGeth: `"0x1234"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newGuestInputCarryFixture(t)
			fixture.verifier = shastaVerifier
			setGuestInputCarryFixtureBlock(t, fixture, 150, 600)
			fixture.witnessChainSpec = guestInputChainSpec(t,
				`{
					"SHASTA": {"Timestamp": 200},
					"UNZEN": {"Timestamp": 500}
				}`,
				fmt.Sprintf(`{
					"SHASTA": {"SgxGeth": %q},
					"UNZEN": {"SgxGeth": %s}
				}`, shastaVerifier, tc.unzenSgxGeth),
			)
			view := decodeGuestInputCarryView(t, fixture)

			err := ValidateGuestInputCarry(view)
			if err == nil || !strings.Contains(err.Error(), "parse witness.chain_spec.verifier_address_forks.UNZEN.Sgx") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGuestInputCarryRejectsUnknownHardForkString(t *testing.T) {
	fixture := newGuestInputCarryFixture(t)
	fixture.witnessChainSpec = guestInputChainSpec(t,
		`{"SHASTA": "later"}`,
		fmt.Sprintf(`{"SHASTA": {"SgxGeth": %q}}`, fixture.verifier),
	)
	view := decodeGuestInputCarryView(t, fixture)

	err := ValidateGuestInputCarry(view)
	if err == nil || !strings.Contains(err.Error(), `unknown hard fork string "later"`) {
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

func TestDecodeGuestInputRejectsMissingOrNullStrictCarryFields(t *testing.T) {
	cases := []struct {
		name    string
		carry   json.RawMessage
		wantErr string
	}{
		{
			name: "missing proposal id",
			carry: strictCarryData(t, `{
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer", "timestamp": 193828690},
				"checkpoint": {"blockNumber": "$checkpointNumber", "blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `missing required field "proposal_id"`,
		},
		{
			name: "null proposal id",
			carry: strictCarryData(t, `{
				"proposal_id": null,
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer", "timestamp": 193828690},
				"checkpoint": {"blockNumber": "$checkpointNumber", "blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `parse field "proposal_id": empty quantity`,
		},
		{
			name: "missing transition timestamp",
			carry: strictCarryData(t, `{
				"proposal_id": 12345,
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer"},
				"checkpoint": {"blockNumber": "$checkpointNumber", "blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `missing required field "timestamp"`,
		},
		{
			name: "null transition timestamp",
			carry: strictCarryData(t, `{
				"proposal_id": 12345,
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer", "timestamp": null},
				"checkpoint": {"blockNumber": "$checkpointNumber", "blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `parse field "timestamp": empty quantity`,
		},
		{
			name: "missing checkpoint block number",
			carry: strictCarryData(t, `{
				"proposal_id": 12345,
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer", "timestamp": 193828690},
				"checkpoint": {"blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `missing required field "blockNumber"`,
		},
		{
			name: "null checkpoint block number",
			carry: strictCarryData(t, `{
				"proposal_id": 12345,
				"proposal_hash": "$proposalHash",
				"parent_proposal_hash": "$parentProposalHash",
				"parent_block_hash": "$parentBlockHash",
				"actual_prover": "$actualProver",
				"transition": {"proposer": "$proposer", "timestamp": 193828690},
				"checkpoint": {"blockNumber": null, "blockHash": "$checkpointBlockHash", "stateRoot": "$checkpointStateRoot"}
			}`),
			wantErr: `parse field "blockNumber": empty quantity`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newGuestInputCarryFixture(t)
			fixture.proofCarryOverride = tc.carry

			_, err := DecodeGuestInput(fixture.input(t))
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHashShastaProposalRejectsNullRequiredArrays(t *testing.T) {
	cases := []struct {
		name    string
		raw     json.RawMessage
		wantErr string
	}{
		{
			name:    "sources",
			raw:     proposalJSONWithSources(t, `null`),
			wantErr: `field "sources" must be an array`,
		},
		{
			name: "blob hashes",
			raw: proposalJSONWithSources(t, `[{
				"isForcedInclusion": true,
				"blobSlice": {
					"blobHashes": null,
					"offset": 0,
					"timestamp": 100
				}
			}]`),
			wantErr: `field "blobHashes" must be an array`,
		},
		{
			name: "is forced inclusion",
			raw: proposalJSONWithSources(t, `[{
				"isForcedInclusion": null,
				"blobSlice": {
					"blobHashes": ["0x67890abcdef1234567890abcdef123451234567890abcdef1234567890abcdef"],
					"offset": 0,
					"timestamp": 100
				}
			}]`),
			wantErr: `missing or null required field "isForcedInclusion"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeShastaProposal(tc.raw)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
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
	proofCarryOverride  json.RawMessage
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
		witnessChainSpec: guestInputChainSpec(t,
			`{"SHASTA": {"Block": 0}}`,
			fmt.Sprintf(`{"SHASTA": {"SGXGETH": %q}}`, verifier),
		),
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
	if len(f.proofCarryOverride) != 0 {
		return f.proofCarryOverride
	}

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

func guestInputChainSpec(t *testing.T, hardForks string, verifierForks string) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": 167013,
		"hard_forks": %s,
		"verifier_address_forks": %s
	}`, hardForks, verifierForks))
}

func setGuestInputCarryFixtureBlock(
	t *testing.T,
	fixture *guestInputCarryFixture,
	number uint64,
	timestamp uint64,
) {
	t.Helper()
	fixture.block = sampleReplayBlockWithTimestamp(
		t,
		number,
		timestamp,
		fixture.parentBlockHash,
		fixture.checkpointStateRoot,
		testHash("33"),
	)
	fixture.checkpointNumber = fmt.Sprintf("0x%x", number)
	fixture.checkpointBlockHash = replayBlockHash(t, fixture.block)
}

func sampleReplayBlockWithTimestamp(
	t *testing.T,
	number uint64,
	timestamp uint64,
	parentHash string,
	stateRoot string,
	receiptsRoot string,
) []byte {
	t.Helper()
	blockRaw := sampleReplayBlock(t, fmt.Sprintf("0x%x", number), parentHash, stateRoot, receiptsRoot)
	decoded, err := decodeBlockEnvelope(blockRaw)
	if err != nil {
		t.Fatalf("decode block envelope: %v", err)
	}
	header, err := decodeJSONObject(decoded.Header)
	if err != nil {
		t.Fatalf("decode header object: %v", err)
	}
	header["timestamp"] = mustRawMessage(t, fmt.Sprintf("%d", timestamp))
	decoded.Header, err = json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal block: %v", err)
	}
	return encoded
}

func strictCarryData(t *testing.T, transitionInputFormat string) json.RawMessage {
	t.Helper()
	fixture := newGuestInputCarryFixture(t)
	transitionInput := strings.NewReplacer(
		"$proposalHash", fixture.proposalHash,
		"$parentProposalHash", fixture.parentProposalHash,
		"$parentBlockHash", fixture.parentBlockHash,
		"$actualProver", fixture.actualProver,
		"$proposer", fixture.proposer,
		"$checkpointNumber", fixture.checkpointNumber,
		"$checkpointBlockHash", fixture.checkpointBlockHash,
		"$checkpointStateRoot", fixture.checkpointStateRoot,
	).Replace(
		transitionInputFormat,
	)
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": 167013,
		"verifier": %q,
		"transition_input": %s
	}`, fixture.verifier, transitionInput))
}

func proposalJSONWithSources(t *testing.T, sources string) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"id": 12345,
		"timestamp": 193828690,
		"endOfSubmissionWindowTimestamp": 193829690,
		"proposer": "0x1234567890abcdef1234567890abcdef12345678",
		"parentProposalHash": "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"originBlockNumber": 73826,
		"originBlockHash": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"basefeeSharingPctg": 42,
		"sources": %s
	}`, sources))
}
