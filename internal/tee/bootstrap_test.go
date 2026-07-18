package tee

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type fakeProvider struct {
	hasKey   bool
	savedKey *ecdsa.PrivateKey
	quote    []byte
	quoteErr error
	calls    []string
}

func (f *fakeProvider) HasPrivateKey() (bool, error) {
	f.calls = append(f.calls, "has")
	return f.hasKey, nil
}

func (f *fakeProvider) LoadQuote(common.Address) (Quote, error) {
	f.calls = append(f.calls, "quote")
	if f.quoteErr != nil {
		return nil, f.quoteErr
	}
	return StaticQuote(f.quote), nil
}

func (f *fakeProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	f.calls = append(f.calls, "load")
	if f.savedKey == nil {
		return nil, errors.New("missing key")
	}
	return f.savedKey, nil
}

func (f *fakeProvider) SavePrivateKey(privKey *ecdsa.PrivateKey) error {
	f.calls = append(f.calls, "save")
	f.savedKey = privKey
	return nil
}

func TestBootstrapRefusesToOverwriteExistingKey(t *testing.T) {
	provider := &fakeProvider{hasKey: true}

	_, err := Bootstrap(provider, false)
	if !errors.Is(err, ErrPrivateKeyExists) {
		t.Fatalf("expected ErrPrivateKeyExists, got %v", err)
	}
	if slices.Contains(provider.calls, "save") {
		t.Fatalf("existing key must not be overwritten, calls: %v", provider.calls)
	}
}

func TestBootstrapForceOverwritesExistingKey(t *testing.T) {
	provider := &fakeProvider{hasKey: true, quote: []byte{0xca, 0xfe}}

	data, err := Bootstrap(provider, true)
	if err != nil {
		t.Fatalf("bootstrap with force: %v", err)
	}
	if provider.savedKey == nil {
		t.Fatalf("expected a new key to be saved")
	}
	if data.InstanceAddress != crypto.PubkeyToAddress(provider.savedKey.PublicKey) {
		t.Fatalf("instance address does not match saved key")
	}
}

func TestBootstrapFetchesQuoteBeforeSavingKey(t *testing.T) {
	provider := &fakeProvider{quoteErr: errors.New("quote unavailable")}

	_, err := Bootstrap(provider, false)
	if err == nil {
		t.Fatalf("expected quote error")
	}
	if slices.Contains(provider.calls, "save") {
		t.Fatalf("key must not be saved when quote fails, calls: %v", provider.calls)
	}
}

func TestBootstrapReturnsIdentityConsistentWithSavedKey(t *testing.T) {
	provider := &fakeProvider{quote: []byte{0xca, 0xfe, 0xba, 0xbe}}

	data, err := Bootstrap(provider, false)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if provider.savedKey == nil {
		t.Fatalf("expected key to be saved")
	}
	if !bytes.Equal(data.PublicKey, crypto.FromECDSAPub(&provider.savedKey.PublicKey)) {
		t.Fatalf("public key does not match saved key")
	}
	if data.InstanceAddress != crypto.PubkeyToAddress(provider.savedKey.PublicKey) {
		t.Fatalf("instance address does not match saved key")
	}
	if !bytes.Equal(data.Quote, []byte{0xca, 0xfe, 0xba, 0xbe}) {
		t.Fatalf("quote mismatch: %x", data.Quote)
	}
}

func TestAtomicWriteFileWritesContentWithPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := atomicWriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected content: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("unexpected perm: %#o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the target file, found %d entries", len(entries))
	}
}

func TestAtomicWriteFileReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priv.key")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	if err := atomicWriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("unexpected content: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected perm: %#o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the target file, found %d entries", len(entries))
	}
}

func TestBootstrapDataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := BootstrapData{
		PublicKey:       []byte{0x04, 0x01, 0x02, 0x03},
		InstanceAddress: common.HexToAddress("0x0000777735367b36bc9b61c50022d9d0700db4ec"),
		Quote:           []byte{0xca, 0xfe, 0xba, 0xbe},
	}

	if err := SaveBootstrapData(dir, want); err != nil {
		t.Fatalf("save bootstrap data: %v", err)
	}

	got, err := LoadBootstrapData(dir)
	if err != nil {
		t.Fatalf("load bootstrap data: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bootstrap data mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	info, err := os.Stat(filepath.Join(dir, bootstrapInfoFilename))
	if err != nil {
		t.Fatalf("stat bootstrap data: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("bootstrap data permissions: got %#o want %#o", got, want)
	}
}

func TestRegisteredForksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := RegisteredForks{
		"shasta": 3131899904,
		"uzen":   42,
	}

	if err := SaveRegisteredForks(dir, want); err != nil {
		t.Fatalf("save registered forks: %v", err)
	}

	got, err := LoadRegisteredForks(dir)
	if err != nil {
		t.Fatalf("load registered forks: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered forks mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	info, err := os.Stat(filepath.Join(dir, registeredInfoFilename))
	if err != nil {
		t.Fatalf("stat registered forks: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("registered forks permissions: got %#o want %#o", got, want)
	}
}
