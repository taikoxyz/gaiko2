package tee

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	attestationInfoFilename = "attestation.gaiko2.json"
)

type AttestationMetadata struct {
	UniqueID        string `json:"unique_id"`
	SignerID        string `json:"signer_id"`
	ProductID       uint32 `json:"product_id"`
	SecurityVersion uint32 `json:"security_version"`
}

func SaveAttestationMetadata(configDir string, data AttestationMetadata) error {
	return writeJSON(filepath.Join(configDir, attestationInfoFilename), data)
}

func LoadAttestationMetadata(configDir string) (AttestationMetadata, error) {
	var data AttestationMetadata
	if err := readJSON(filepath.Join(configDir, attestationInfoFilename), &data); err != nil {
		return AttestationMetadata{}, err
	}
	return data, nil
}

func ReadAttestationMetadataFile(path string) (AttestationMetadata, error) {
	var data AttestationMetadata
	contents, err := os.ReadFile(path)
	if err != nil {
		return AttestationMetadata{}, err
	}
	if err := json.Unmarshal(contents, &data); err != nil {
		return AttestationMetadata{}, err
	}
	return data, nil
}
