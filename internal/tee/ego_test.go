package tee

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEGoProviderHasPrivateKeyFalseWhenAbsent(t *testing.T) {
	provider := NewEGoProvider(t.TempDir())

	exists, err := provider.HasPrivateKey()
	if err != nil {
		t.Fatalf("has private key: %v", err)
	}
	if exists {
		t.Fatalf("expected no key in empty secret dir")
	}
}

func TestEGoProviderHasPrivateKeyTrueWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, privateKeyFilename), []byte("sealed"), 0o600); err != nil {
		t.Fatalf("write sealed key: %v", err)
	}
	provider := NewEGoProvider(dir)

	exists, err := provider.HasPrivateKey()
	if err != nil {
		t.Fatalf("has private key: %v", err)
	}
	if !exists {
		t.Fatalf("expected existing sealed key to be detected")
	}
}
