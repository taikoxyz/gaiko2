package prover

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

type manifestVectorCorpus struct {
	SchemaVersion int              `json:"schema_version"`
	Cases         []manifestVector `json:"cases"`
}

type manifestVector struct {
	Name             string `json:"name"`
	Family           string `json:"family"`
	PayloadHex       string `json:"payload_hex"`
	Offset           uint64 `json:"offset"`
	ExpectDefault    bool   `json:"expect_default"`
	ExpectedManifest string `json:"expected_manifest_rlp_hex"`
}

func loadManifestVectors(t *testing.T) []manifestVector {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve manifest vector test path")
	}
	raw, err := os.ReadFile(filepath.Join(
		filepath.Dir(file),
		"../../testdata/derivation_vectors/manifest_cases.json",
	))
	if err != nil {
		t.Fatalf("read manifest vectors: %v", err)
	}

	var corpus manifestVectorCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("decode manifest vectors: %v", err)
	}
	if corpus.SchemaVersion != 1 {
		t.Fatalf("manifest vector schema version %d, want 1", corpus.SchemaVersion)
	}
	if len(corpus.Cases) != 136 {
		t.Fatalf("manifest vector count %d, want 136", len(corpus.Cases))
	}

	familyCounts := make(map[string]int)
	for _, vector := range corpus.Cases {
		familyCounts[vector.Family]++
	}
	for family, want := range map[string]int{"f8": 17, "f9": 94, "framing": 25} {
		if got := familyCounts[family]; got != want {
			t.Fatalf("manifest vector family %q count %d, want %d", family, got, want)
		}
		delete(familyCounts, family)
	}
	if len(familyCounts) != 0 {
		t.Fatalf("unexpected manifest vector families: %v", familyCounts)
	}
	return corpus.Cases
}

func manifestVectorBytes(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode vector hex: %v", err)
	}
	return raw
}

func decodeManifestVector(t *testing.T, vector manifestVector) shastaSourceManifest {
	t.Helper()
	payload := manifestVectorBytes(t, vector.PayloadHex)
	manifest, err := decodeBlobBackedSourceManifest(
		blobSourceDataView{TxDataFromBlob: [][]byte{encodeTestKonaBlob(t, payload)}},
		vector.Offset,
		shastaUnzenDerivationSourceLimit,
	)
	if err != nil {
		t.Fatalf("decode manifest vector: %v", err)
	}
	return manifest
}

func encodeManifestVectorResult(t *testing.T, manifest shastaSourceManifest) []byte {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(manifest)
	if err != nil {
		t.Fatalf("encode decoded manifest: %v", err)
	}
	return encoded
}

// A lost grammar or zlib check retains a hostile source, or defaults a valid one,
// and therefore disagrees with the independently generated expected manifest RLP.
func TestManifestVectors(t *testing.T) {
	vectors := loadManifestVectors(t)
	for _, family := range []string{"f9", "framing", "f8"} {
		t.Run(family, func(t *testing.T) {
			for _, vector := range vectors {
				if vector.Family != family {
					continue
				}
				vector := vector
				t.Run(vector.Name, func(t *testing.T) {
					manifest := decodeManifestVector(t, vector)
					if got := isDefaultSourceManifest(manifest); got != vector.ExpectDefault {
						t.Fatalf("default=%t, want %t", got, vector.ExpectDefault)
					}
					got := encodeManifestVectorResult(t, manifest)
					want := manifestVectorBytes(t, vector.ExpectedManifest)
					if !bytes.Equal(got, want) {
						t.Fatalf("manifest RLP %x, want %x", got, want)
					}
				})
			}
		})
	}
}

func manifestVectorByName(t *testing.T, name string) manifestVector {
	t.Helper()
	for _, vector := range loadManifestVectors(t) {
		if vector.Name == name {
			return vector
		}
	}
	t.Fatalf("manifest vector %q not found", name)
	return manifestVector{}
}

func prepareManifestVector(
	t *testing.T,
	vector manifestVector,
	forced bool,
	proposalTimestamp uint64,
) shastaSourceManifest {
	t.Helper()
	parent := shastaManifestParentContext{
		Header: &types.Header{
			Number:   big.NewInt(0),
			Time:     99,
			GasLimit: shastaMinBlockGasLimit,
		},
		AnchorBlockNumber: 4,
	}
	proposal := shastaProposalView{
		Timestamp:         proposalTimestamp,
		Proposer:          common.HexToAddress("0x9999"),
		OriginBlockNumber: 5,
	}
	source := shastaDerivationSourceView{
		IsForcedInclusion: forced,
		BlobSlice: shastaBlobSliceView{
			BlobHashes: []common.Hash{{1}},
			Offset:     vector.Offset,
		},
	}
	payload := manifestVectorBytes(t, vector.PayloadHex)
	manifest, err := prepareSourceManifest(
		blobSourceDataView{TxDataFromBlob: [][]byte{encodeTestKonaBlob(t, payload)}},
		source,
		parent,
		proposal,
		params.TaikoInternalNetworkID.Uint64(),
		0,
		shastaUnzenDerivationSourceLimit,
	)
	if err != nil {
		t.Fatalf("prepare manifest vector: %v", err)
	}
	return manifest
}

func assertInheritedDefaultManifest(t *testing.T, manifest shastaSourceManifest) {
	t.Helper()
	if len(manifest.Blocks) != 1 {
		t.Fatalf("inherited default block count %d, want 1", len(manifest.Blocks))
	}
	block := manifest.Blocks[0]
	if block.Timestamp != 100 ||
		block.Coinbase != common.HexToAddress("0x9999") ||
		block.AnchorBlockNumber != 4 ||
		block.GasLimit != shastaMinBlockGasLimit ||
		len(block.Transactions) != 0 {
		t.Fatalf("unexpected inherited default block: %+v", block)
	}
}

func TestManifestVectorsDefaultWholeSourceAndPreserveNeighbor(t *testing.T) {
	invalid := manifestVectorByName(t, "type2_parity_2")
	gotInvalid := prepareManifestVector(t, invalid, false, 100)
	assertInheritedDefaultManifest(t, gotInvalid)

	control := manifestVectorByName(t, "type2_control")
	gotControl := prepareManifestVector(t, control, false, 100)
	wantControl := manifestVectorBytes(t, control.ExpectedManifest)
	if got := encodeManifestVectorResult(t, gotControl); !bytes.Equal(got, wantControl) {
		t.Fatalf("valid neighboring source RLP %x, want %x", got, wantControl)
	}

	lateInvalid := decodeManifestVector(t, manifestVectorByName(t, "late_bad_transaction"))
	if !isDefaultSourceManifest(lateInvalid) {
		t.Fatalf("late bad transaction retained part of its source: %+v", lateInvalid)
	}
}

func TestManifestVectorsForcedMultiBlockDefaultsAndInherits(t *testing.T) {
	vector := manifestVectorByName(t, "two_valid_blocks")
	manifest := prepareManifestVector(t, vector, true, 101)
	assertInheritedDefaultManifest(t, manifest)
}
