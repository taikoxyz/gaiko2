package tee

import "testing"

func TestSaveAndLoadAttestationMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := AttestationMetadata{
		UniqueID:        "abc123",
		SignerID:        "def456",
		ProductID:       1,
		SecurityVersion: 2,
	}

	if err := SaveAttestationMetadata(dir, want); err != nil {
		t.Fatalf("save attestation metadata: %v", err)
	}

	got, err := LoadAttestationMetadata(dir)
	if err != nil {
		t.Fatalf("load attestation metadata: %v", err)
	}

	if got != want {
		t.Fatalf("attestation metadata mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
