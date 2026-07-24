package tee

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type fakeProvider struct {
	hasKey    bool
	savedKey  *ecdsa.PrivateKey
	loadErr   error
	quote     []byte
	quoteErr  error
	saveErr   error
	overwrite bool
	calls     []string
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
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.savedKey == nil {
		return nil, errors.New("missing key")
	}
	return f.savedKey, nil
}

func TestBootstrapDataForExistingKeyMarksUnreadablePrivateKey(t *testing.T) {
	unsealErr := errors.New("sealed key belongs to another enclave")
	provider := &fakeProvider{loadErr: unsealErr}

	_, _, err := BootstrapDataForExistingKey(provider, t.TempDir())
	if !errors.Is(err, ErrPrivateKeyUnavailable) {
		t.Fatalf("expected ErrPrivateKeyUnavailable, got %v", err)
	}
	if !errors.Is(err, unsealErr) {
		t.Fatalf("expected unseal cause, got %v", err)
	}
}

func (f *fakeProvider) SavePrivateKey(privKey *ecdsa.PrivateKey, overwrite bool) error {
	f.calls = append(f.calls, "save")
	f.overwrite = overwrite
	if f.saveErr != nil {
		return f.saveErr
	}
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

func TestBootstrapReturnsFinalInstallCollision(t *testing.T) {
	provider := &fakeProvider{
		hasKey:  false,
		quote:   []byte{0xca, 0xfe},
		saveErr: ErrPrivateKeyExists,
	}
	_, err := Bootstrap(provider, false)
	if !errors.Is(err, ErrPrivateKeyExists) {
		t.Fatalf("expected final install collision, got %v", err)
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

func TestBootstrapDataForExistingKeyRebuildsMissingMetadata(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := &fakeProvider{savedKey: key, quote: []byte{0xca, 0xfe}}

	data, matches, err := BootstrapDataForExistingKey(provider, t.TempDir())
	if err != nil {
		t.Fatalf("recover bootstrap data: %v", err)
	}
	if matches {
		t.Fatal("missing metadata must require recovery")
	}
	if data.InstanceAddress != crypto.PubkeyToAddress(key.PublicKey) {
		t.Fatalf("recovered identity mismatch: %s", data.InstanceAddress)
	}
	if slices.Contains(provider.calls, "save") {
		t.Fatalf("recovery rotated key, calls: %v", provider.calls)
	}
}

func TestBootstrapDataForExistingKeyKeepsMatchingMetadata(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dir := t.TempDir()
	want := BootstrapData{
		PublicKey:       crypto.FromECDSAPub(&key.PublicKey),
		InstanceAddress: crypto.PubkeyToAddress(key.PublicKey),
		Quote:           []byte{0xca, 0xfe},
	}
	if err := SaveBootstrapData(dir, want); err != nil {
		t.Fatalf("save bootstrap data: %v", err)
	}
	provider := &fakeProvider{savedKey: key}

	got, matches, err := BootstrapDataForExistingKey(provider, dir)
	if err != nil {
		t.Fatalf("check bootstrap data: %v", err)
	}
	if !matches || !reflect.DeepEqual(got, want) {
		t.Fatalf("got (%+v, %v), want (%+v, true)", got, matches, want)
	}
	if slices.Contains(provider.calls, "quote") {
		t.Fatalf("matching metadata should not fetch a quote: %v", provider.calls)
	}
}

func TestBootstrapDataForExistingKeyRebuildsMetadataWithoutQuote(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dir := t.TempDir()
	incomplete := BootstrapData{
		PublicKey:       crypto.FromECDSAPub(&key.PublicKey),
		InstanceAddress: crypto.PubkeyToAddress(key.PublicKey),
	}
	if err := SaveBootstrapData(dir, incomplete); err != nil {
		t.Fatalf("save incomplete bootstrap data: %v", err)
	}
	provider := &fakeProvider{
		savedKey: key,
		quote:    []byte{0xca, 0xfe},
	}

	got, matches, err := BootstrapDataForExistingKey(provider, dir)
	if err != nil {
		t.Fatalf("rebuild bootstrap data: %v", err)
	}
	if matches {
		t.Fatal("metadata without a quote must require recovery")
	}
	if !bytes.Equal(got.Quote, provider.quote) {
		t.Fatalf("recovered quote=%x want=%x", got.Quote, provider.quote)
	}
}

func TestSaveBootstrapDataSyncsParentsOfNewDirectories(t *testing.T) {
	previous := syncDirectoryFn
	t.Cleanup(func() { syncDirectoryFn = previous })

	root := t.TempDir()
	configDir := filepath.Join(root, "new", "nested")
	var synced []string
	syncDirectoryFn = func(path string) error {
		synced = append(synced, filepath.Clean(path))
		return nil
	}

	if err := SaveBootstrapData(configDir, BootstrapData{}); err != nil {
		t.Fatalf("save bootstrap data: %v", err)
	}
	for _, want := range []string{root, filepath.Join(root, "new")} {
		if !slices.Contains(synced, filepath.Clean(want)) {
			t.Fatalf("directory %s was not synced; synced=%v", want, synced)
		}
	}
}

func TestEnsureDirectorySyncsExistingDirectoryAncestors(t *testing.T) {
	previous := syncDirectoryFn
	t.Cleanup(func() { syncDirectoryFn = previous })

	root := t.TempDir()
	dirPath := filepath.Join(root, "existing", "nested")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("create directory tree: %v", err)
	}
	if err := os.Chmod(dirPath, 0o755); err != nil {
		t.Fatalf("set existing directory mode: %v", err)
	}
	var synced []string
	syncDirectoryFn = func(path string) error {
		synced = append(synced, filepath.Clean(path))
		return nil
	}

	if err := ensureDirectory(dirPath, 0o700); err != nil {
		t.Fatalf("ensure existing directory: %v", err)
	}
	for _, want := range []string{dirPath, filepath.Dir(dirPath), root} {
		if !slices.Contains(synced, filepath.Clean(want)) {
			t.Fatalf("directory %s was not synced; synced=%v", want, synced)
		}
	}
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat existing directory: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o755); got != want {
		t.Fatalf("existing directory mode changed: got %#o want %#o", got, want)
	}
}

func TestSyncDirectoryAncestorsStopsAtMissingMountParent(t *testing.T) {
	previous := syncDirectoryFn
	t.Cleanup(func() { syncDirectoryFn = previous })

	mountDir := filepath.Join(t.TempDir(), "mounted")
	mountParent := filepath.Dir(mountDir)
	var synced []string
	syncDirectoryFn = func(path string) error {
		path = filepath.Clean(path)
		synced = append(synced, path)
		if path == mountParent {
			return fs.ErrNotExist
		}
		return nil
	}

	if err := syncDirectoryAncestors(mountDir); err != nil {
		t.Fatalf("sync mount directory ancestors: %v", err)
	}
	if want := []string{filepath.Clean(mountDir), filepath.Clean(mountParent)}; !slices.Equal(synced, want) {
		t.Fatalf("synced=%v want=%v", synced, want)
	}
}

func TestEnsureDirectoryRetriesSyncAfterCreationFailure(t *testing.T) {
	previous := syncDirectoryFn
	t.Cleanup(func() { syncDirectoryFn = previous })

	root := t.TempDir()
	dirPath := filepath.Join(root, "new", "nested")
	syncErr := errors.New("sync failed")
	syncDirectoryFn = func(string) error { return syncErr }

	if err := ensureDirectory(dirPath, 0o700); !errors.Is(err, syncErr) {
		t.Fatalf("first ensure error = %v, want %v", err, syncErr)
	}
	if info, err := os.Stat(dirPath); err != nil || !info.IsDir() {
		t.Fatalf("directory was not created before sync failure: info=%v err=%v", info, err)
	}

	var synced []string
	syncDirectoryFn = func(path string) error {
		synced = append(synced, filepath.Clean(path))
		return nil
	}
	if err := ensureDirectory(dirPath, 0o700); err != nil {
		t.Fatalf("retry ensure directory: %v", err)
	}
	for _, want := range []string{dirPath, filepath.Dir(dirPath), root} {
		if !slices.Contains(synced, filepath.Clean(want)) {
			t.Fatalf("directory %s was not re-synced; synced=%v", want, synced)
		}
	}
}

func TestAtomicWriteFileWritesContentWithPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := atomicWriteFile(path, []byte("hello"), 0o644, true); err != nil {
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

func TestCreateTempFileUsesRequestedPermissions(t *testing.T) {
	for _, wantPerm := range []os.FileMode{0o600, 0o644} {
		t.Run(wantPerm.String(), func(t *testing.T) {
			file, err := createTempFile(t.TempDir(), "bootstrap.tmp-", wantPerm)
			if err != nil {
				t.Fatalf("create temp file: %v", err)
			}
			path := file.Name()
			if err := file.Close(); err != nil {
				t.Fatalf("close temp file: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat temp file: %v", err)
			}
			if got := info.Mode().Perm(); got != wantPerm {
				t.Fatalf("temp file permissions: got %#o want %#o", got, wantPerm)
			}
		})
	}
}

func TestAtomicWriteFileReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priv.key")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	if err := atomicWriteFile(path, []byte("new"), 0o600, true); err != nil {
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

func TestAtomicWriteFileNoReplacePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priv.key")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	err := atomicWriteFile(path, []byte("new"), 0o600, false)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("existing file changed: %q", got)
	}
}

func TestAtomicWriteFileConcurrentNoReplaceHasOneWinner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priv.key")
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, value := range []string{"first", "second"} {
		value := value
		go func() {
			<-start
			results <- atomicWriteFile(path, []byte(value), 0o600, false)
		}()
	}
	close(start)

	var successes, collisions int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, fs.ErrExist):
			collisions++
		default:
			t.Fatalf("unexpected write error: %v", err)
		}
	}
	if successes != 1 || collisions != 1 {
		t.Fatalf("successes=%d collisions=%d", successes, collisions)
	}
}

func TestAtomicWriteFileReportsDirectorySyncFailure(t *testing.T) {
	previous := syncDirectoryFn
	t.Cleanup(func() { syncDirectoryFn = previous })
	sentinel := errors.New("directory sync failed")
	syncDirectoryFn = func(string) error { return sentinel }

	err := atomicWriteFile(
		filepath.Join(t.TempDir(), "bootstrap.json"),
		[]byte("{}"),
		0o644,
		true,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected directory sync failure, got %v", err)
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
