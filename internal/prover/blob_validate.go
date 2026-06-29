package prover

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
)

type blobSourceDataView struct {
	TxDataFromCalldata     []byte
	TxDataFromBlob         [][]byte
	BlobCommitments        [][]byte
	BlobCommitmentsPresent bool
	BlobProofs             [][]byte
}

func ValidateGuestInputBlobSources(view *GuestInputView) error {
	if view == nil {
		return fmt.Errorf("guest input view is nil")
	}

	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		return err
	}
	if len(proposal.Sources) == 0 {
		return nil
	}
	if len(view.DataSourcesRaw) != len(proposal.Sources) {
		return fmt.Errorf(
			"data source count mismatch: proposal_sources=%d data_sources=%d",
			len(proposal.Sources),
			len(view.DataSourcesRaw),
		)
	}

	for sourceIndex, source := range proposal.Sources {
		dataSource, err := decodeBlobSourceData(view.DataSourcesRaw[sourceIndex])
		if err != nil {
			return fmt.Errorf("decode data_sources[%d]: %w", sourceIndex, err)
		}

		expectedBlobHashes := source.BlobSlice.BlobHashes
		if len(expectedBlobHashes) == 0 {
			if err := requireEmptyNonBlobDataSource(sourceIndex, dataSource); err != nil {
				return err
			}
			continue
		}

		if len(dataSource.TxDataFromBlob) != len(expectedBlobHashes) {
			return fmt.Errorf(
				"data_sources[%d].tx_data_from_blob count mismatch: got %d want %d",
				sourceIndex,
				len(dataSource.TxDataFromBlob),
				len(expectedBlobHashes),
			)
		}
		if dataSource.BlobCommitmentsPresent && len(dataSource.BlobCommitments) != len(dataSource.TxDataFromBlob) {
			return fmt.Errorf(
				"data_sources[%d].blob_commitments count mismatch: got %d want %d",
				sourceIndex,
				len(dataSource.BlobCommitments),
				len(dataSource.TxDataFromBlob),
			)
		}

		for blobIndex, rawBlob := range dataSource.TxDataFromBlob {
			commitment, computedHash, err := deriveBlobCommitmentAndHash(rawBlob)
			if err != nil {
				return fmt.Errorf("data_sources[%d].tx_data_from_blob[%d]: %w", sourceIndex, blobIndex, err)
			}
			if computedHash != expectedBlobHashes[blobIndex] {
				return fmt.Errorf(
					"data_sources[%d].tx_data_from_blob[%d] blob hash mismatch: computed=%s proposal=%s",
					sourceIndex,
					blobIndex,
					computedHash.Hex(),
					expectedBlobHashes[blobIndex].Hex(),
				)
			}

			if dataSource.BlobCommitmentsPresent {
				provided := dataSource.BlobCommitments[blobIndex]
				if len(provided) != len(kzg4844.Commitment{}) {
					return fmt.Errorf(
						"data_sources[%d].blob_commitments[%d] must be 48 bytes, got %d",
						sourceIndex,
						blobIndex,
						len(provided),
					)
				}
				if !bytes.Equal(provided, commitment[:]) {
					return fmt.Errorf("data_sources[%d].blob_commitments[%d] mismatch", sourceIndex, blobIndex)
				}
			}
		}
	}

	return nil
}

func decodeBlobSourceData(raw json.RawMessage) (blobSourceDataView, error) {
	if isEmptyOrNullRawMessage(raw) {
		return blobSourceDataView{}, fmt.Errorf("data source must be an object")
	}
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return blobSourceDataView{}, fmt.Errorf("unmarshal data source: %w", err)
	}

	calldata, err := optionalByteSliceField(fields, "tx_data_from_calldata")
	if err != nil {
		return blobSourceDataView{}, err
	}
	blobData, _, err := optionalByteSlicesField(fields, "tx_data_from_blob")
	if err != nil {
		return blobSourceDataView{}, err
	}
	commitments, commitmentsPresent, err := optionalByteSlicesField(fields, "blob_commitments")
	if err != nil {
		return blobSourceDataView{}, err
	}
	proofs, _, err := optionalByteSlicesField(fields, "blob_proofs")
	if err != nil {
		return blobSourceDataView{}, err
	}

	return blobSourceDataView{
		TxDataFromCalldata:     calldata,
		TxDataFromBlob:         blobData,
		BlobCommitments:        commitments,
		BlobCommitmentsPresent: commitmentsPresent,
		BlobProofs:             proofs,
	}, nil
}

func optionalByteSliceField(fields map[string]json.RawMessage, name string) ([]byte, error) {
	raw, ok := lookupField(fields, name)
	if !ok {
		return nil, nil
	}
	if isEmptyOrNullRawMessage(raw) {
		return nil, fmt.Errorf("%s must be bytes", name)
	}
	value, err := parseFlexibleBytesJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}

func optionalByteSlicesField(fields map[string]json.RawMessage, name string) ([][]byte, bool, error) {
	raw, ok := lookupField(fields, name)
	if !ok {
		return nil, false, nil
	}
	if isEmptyOrNullRawMessage(raw) {
		return nil, true, fmt.Errorf("%s must be an array", name)
	}

	var rawValues []json.RawMessage
	if err := json.Unmarshal(raw, &rawValues); err != nil {
		return nil, true, fmt.Errorf("%s must be an array: %w", name, err)
	}
	values := make([][]byte, len(rawValues))
	for i, rawValue := range rawValues {
		value, err := parseFlexibleBytesJSON(rawValue)
		if err != nil {
			return nil, true, fmt.Errorf("%s[%d]: %w", name, i, err)
		}
		values[i] = value
	}
	return values, true, nil
}

func parseFlexibleBytesJSON(raw json.RawMessage) ([]byte, error) {
	if isEmptyOrNullRawMessage(raw) {
		return nil, fmt.Errorf("empty bytes value")
	}
	trimmed := bytes.TrimSpace(raw)
	switch trimmed[0] {
	case '"':
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return nil, err
		}
		decoded, err := hexutil.Decode(value)
		if err != nil {
			return nil, fmt.Errorf("invalid hex string: %w", err)
		}
		return decoded, nil
	case '[':
		return parseByteArrayJSON(trimmed)
	default:
		return nil, fmt.Errorf("bytes must be a hex string or byte array")
	}
}

func parseByteArrayJSON(raw json.RawMessage) ([]byte, error) {
	var rawValues []json.RawMessage
	if err := json.Unmarshal(raw, &rawValues); err != nil {
		return nil, err
	}
	value := make([]byte, len(rawValues))
	for i, rawByte := range rawValues {
		if isEmptyOrNullRawMessage(rawByte) {
			return nil, fmt.Errorf("invalid byte value at index %d: null", i)
		}
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(rawByte))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return nil, fmt.Errorf("invalid byte value at index %d: %w", i, err)
		}
		parsed, err := strconv.ParseUint(number.String(), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid byte value at index %d: %w", i, err)
		}
		value[i] = byte(parsed)
	}
	return value, nil
}

func requireEmptyNonBlobDataSource(sourceIndex int, dataSource blobSourceDataView) error {
	if len(dataSource.TxDataFromCalldata) != 0 {
		return fmt.Errorf("data_sources[%d].tx_data_from_calldata must be empty for source without blob hashes", sourceIndex)
	}
	if len(dataSource.TxDataFromBlob) != 0 {
		return fmt.Errorf("data_sources[%d].tx_data_from_blob must be empty for source without blob hashes", sourceIndex)
	}
	if len(dataSource.BlobCommitments) != 0 {
		return fmt.Errorf("data_sources[%d].blob_commitments must be empty for source without blob hashes", sourceIndex)
	}
	if len(dataSource.BlobProofs) != 0 {
		return fmt.Errorf("data_sources[%d].blob_proofs must be empty for source without blob hashes", sourceIndex)
	}
	return nil
}

func deriveBlobCommitmentAndHash(raw []byte) (kzg4844.Commitment, common.Hash, error) {
	var blob kzg4844.Blob
	if len(raw) != len(blob) {
		return kzg4844.Commitment{}, common.Hash{}, fmt.Errorf("raw blob must be %d bytes, got %d", len(blob), len(raw))
	}
	copy(blob[:], raw)

	commitment, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		return kzg4844.Commitment{}, common.Hash{}, fmt.Errorf("compute blob commitment: %w", err)
	}
	versionedHash := kzg4844.CalcBlobHashV1(sha256.New(), &commitment)
	return commitment, common.BytesToHash(versionedHash[:]), nil
}
