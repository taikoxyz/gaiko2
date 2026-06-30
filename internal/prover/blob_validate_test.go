package prover

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
)

func TestValidateGuestInputBlobSourcesAcceptsSerdeByteArrayBlob(t *testing.T) {
	blob := testKZGBlobBytes(0x11)
	_, blobHash := testBlobCommitmentAndHash(t, blob)
	view := decodeBlobSourceGuestInputView(t,
		blobSourceJSON(blobHash),
		fmt.Sprintf(`[{"tx_data_from_blob":[%s]}]`, byteArrayJSON(t, blob)),
	)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate guest input blob sources: %v", err)
	}
}

func TestValidateGuestInputBlobSourcesAcceptsHexStringBlob(t *testing.T) {
	blob := testKZGBlobBytes(0x22)
	commitment, blobHash := testBlobCommitmentAndHash(t, blob)
	view := decodeBlobSourceGuestInputView(t,
		blobSourceJSON(blobHash),
		fmt.Sprintf(
			`[{"tx_data_from_blob":[%s],"blob_commitments":[%s]}]`,
			hexStringJSON(blob),
			hexStringJSON(commitment),
		),
	)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate guest input blob sources: %v", err)
	}
}

func TestValidateGuestInputBlobSourcesAcceptsEmptyProposalSourcesWithEmptyDataSources(t *testing.T) {
	view := decodeBlobSourceGuestInputView(t, `[]`, `[]`)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate guest input blob sources: %v", err)
	}
}

func TestValidateGuestInputBlobSourcesRejectsBlobSourceMismatches(t *testing.T) {
	blob := testKZGBlobBytes(0x33)
	commitment, blobHash := testBlobCommitmentAndHash(t, blob)
	otherBlob := testKZGBlobBytes(0x44)
	_, otherBlobHash := testBlobCommitmentAndHash(t, otherBlob)
	proof := testKZGProofBytes(0x55)

	mutatedBlob := append([]byte(nil), blob...)
	mutatedBlob[31]++

	badCommitment := append([]byte(nil), commitment...)
	badCommitment[0] ^= 0x01

	cases := []struct {
		name        string
		sourcesJSON string
		dataSources string
		wantErr     string
	}{
		{
			name:        "missing blob data rejected for blob backed source",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: `[{}]`,
			wantErr:     "tx_data_from_blob count mismatch",
		},
		{
			name:        "caller supplied commitment and proof are not trusted without raw blob",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: fmt.Sprintf(
				`[{"blob_commitments":[%s],"blob_proofs":[%s]}]`,
				hexStringJSON(commitment),
				hexStringJSON(proof),
			),
			wantErr: "tx_data_from_blob count mismatch",
		},
		{
			name:        "blob count mismatch rejected",
			sourcesJSON: blobSourceJSON(blobHash, otherBlobHash),
			dataSources: fmt.Sprintf(`[{"tx_data_from_blob":[%s]}]`, hexStringJSON(blob)),
			wantErr:     "tx_data_from_blob count mismatch",
		},
		{
			name:        "raw blob mutation rejected even with unchanged supplied fields",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: fmt.Sprintf(
				`[{"tx_data_from_blob":[%s],"blob_commitments":[%s],"blob_proofs":[%s]}]`,
				hexStringJSON(mutatedBlob),
				hexStringJSON(commitment),
				hexStringJSON(proof),
			),
			wantErr: "blob hash mismatch",
		},
		{
			name:        "supplied commitment mismatch rejected",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: fmt.Sprintf(
				`[{"tx_data_from_blob":[%s],"blob_commitments":[%s]}]`,
				hexStringJSON(blob),
				hexStringJSON(badCommitment),
			),
			wantErr: "blob_commitments[0] mismatch",
		},
		{
			name:        "proposal source count mismatch rejected",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: `[]`,
			wantErr:     "data source count mismatch",
		},
		{
			name:        "extra data source rejected when proposal has no sources",
			sourcesJSON: `[]`,
			dataSources: `[{}]`,
			wantErr:     "data source count mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := decodeBlobSourceGuestInputView(t, tc.sourcesJSON, tc.dataSources)

			err := ValidateGuestInputBlobSources(view)
			assertBlobValidationError(t, err, tc.wantErr)
		})
	}
}

func TestValidateGuestInputBlobSourcesRejectsMalformedDataSources(t *testing.T) {
	blob := testKZGBlobBytes(0x66)
	_, blobHash := testBlobCommitmentAndHash(t, blob)

	cases := []struct {
		name        string
		sourcesJSON string
		dataSources string
		wantErr     string
	}{
		{
			name:        "null blob array",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: `[{"tx_data_from_blob":null}]`,
			wantErr:     "tx_data_from_blob must be an array",
		},
		{
			name:        "non byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[256]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "negative byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[-1]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "float byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[1.5]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "string byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":["1"]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "bool byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[true]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "object byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[{}]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "null byte array value",
			sourcesJSON: emptyBlobSourceJSON(),
			dataSources: `[{"tx_data_from_calldata":[null]}]`,
			wantErr:     "invalid byte value",
		},
		{
			name:        "invalid hex blob",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: `[{"tx_data_from_blob":["0xzz"]}]`,
			wantErr:     "invalid hex",
		},
		{
			name:        "bad commitment length",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: fmt.Sprintf(
				`[{"tx_data_from_blob":[%s],"blob_commitments":["0x1234"]}]`,
				hexStringJSON(blob),
			),
			wantErr: "blob_commitments[0] must be 48 bytes",
		},
		{
			name:        "malformed proof bytes",
			sourcesJSON: blobSourceJSON(blobHash),
			dataSources: fmt.Sprintf(
				`[{"tx_data_from_blob":[%s],"blob_proofs":[null]}]`,
				hexStringJSON(blob),
			),
			wantErr: "blob_proofs[0]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := decodeBlobSourceGuestInputView(t, tc.sourcesJSON, tc.dataSources)

			err := ValidateGuestInputBlobSources(view)
			assertBlobValidationError(t, err, tc.wantErr)
		})
	}
}

func decodeBlobSourceGuestInputView(t *testing.T, sourcesJSON string, dataSourcesJSON string) *GuestInputView {
	t.Helper()

	fixture := newGuestInputCarryFixture(t)
	fixture.taiko = mustRawMessage(t, fmt.Sprintf(`{
		"chain_spec": {
			"chain_id": 167013
		},
		"proposal_id": 12345,
		"proposal_event": {
			"proposal": %s
		},
		"prover_data": {
			"actual_prover": %q
		},
		"data_sources": %s
	}`, proposalJSONWithSources(t, sourcesJSON), fixture.actualProver, dataSourcesJSON))
	return decodeGuestInputCarryView(t, fixture)
}

func blobSourceJSON(blobHashes ...string) string {
	hashes := make([]string, len(blobHashes))
	for i, hash := range blobHashes {
		hashes[i] = fmt.Sprintf("%q", hash)
	}
	return fmt.Sprintf(`[{
		"isForcedInclusion": false,
		"blobSlice": {
			"blobHashes": [%s],
			"offset": 0,
			"timestamp": 100
		}
	}]`, strings.Join(hashes, ","))
}

func emptyBlobSourceJSON() string {
	return blobSourceJSON()
}

func testKZGBlobBytes(seed byte) []byte {
	var blob kzg4844.Blob
	blob[31] = seed
	blob[63] = seed + 1
	blob[95] = seed + 2
	blob[len(blob)-1] = seed + 3
	return blob[:]
}

func testBlobCommitmentAndHash(t *testing.T, raw []byte) ([]byte, string) {
	t.Helper()

	var blob kzg4844.Blob
	if len(raw) != len(blob) {
		t.Fatalf("test blob length: got %d want %d", len(raw), len(blob))
	}
	copy(blob[:], raw)
	commitment, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		t.Fatalf("blob to commitment: %v", err)
	}
	versionedHash := kzg4844.CalcBlobHashV1(sha256.New(), &commitment)
	return append([]byte(nil), commitment[:]...), common.BytesToHash(versionedHash[:]).Hex()
}

func testKZGProofBytes(seed byte) []byte {
	proof := make([]byte, 48)
	for i := range proof {
		proof[i] = seed + byte(i%7)
	}
	return proof
}

func byteArrayJSON(t *testing.T, value []byte) string {
	t.Helper()

	encoded := make([]int, len(value))
	for i, b := range value {
		encoded[i] = int(b)
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("marshal byte array: %v", err)
	}
	return string(raw)
}

func hexStringJSON(value []byte) string {
	return fmt.Sprintf("%q", "0x"+hex.EncodeToString(value))
}

func assertBlobValidationError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}
