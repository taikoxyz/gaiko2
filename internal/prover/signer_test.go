package prover

import (
	"crypto/ecdsa"
	"errors"
	"io/fs"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

func TestNativeProofSignerIsSelfConsistentAndDistinctFromGoldenTouch(t *testing.T) {
	signer := NewNativeProofSigner(shastaNativeMockInstance)
	hash := crypto.Keccak256Hash([]byte("gaiko2-native"))

	output, err := signer.SignHash(hash)
	if err != nil {
		t.Fatalf("sign hash: %v", err)
	}
	if len(output.Quote) != 0 {
		t.Fatalf("native proof should not carry quote, got %x", output.Quote)
	}
	if output.InstanceID != shastaNativeMockInstance {
		t.Fatalf("unexpected instance id: %x", output.InstanceID)
	}

	publicKey, err := crypto.UnmarshalPubkey(output.PublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	recovered, err := crypto.SigToPub(hash.Bytes(), normalizeRecoveryID(output.Signature))
	if err != nil {
		t.Fatalf("recover signature: %v", err)
	}
	if crypto.PubkeyToAddress(*publicKey) != output.InstanceAddress {
		t.Fatalf("public key does not match instance address")
	}
	if crypto.PubkeyToAddress(*recovered) != output.InstanceAddress {
		t.Fatalf("signature recovered wrong signer")
	}
	// Keep native proof fixtures independent of the GoldenTouch anchor identity.
	if output.InstanceAddress == shastaGoldenTouchAccount {
		t.Fatal("native proof signer must not reuse the GoldenTouch anchor account")
	}
	if nativeProofPrivateKey == shastaGoldenTouchPrivateKey {
		t.Fatal("native proof key must be distinct from the GoldenTouch anchor key")
	}
}

func TestTEEProofSignerOmitsQuoteDuringProving(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer := NewTEEProofSigner(privateKey, 0x12345678)
	hash := crypto.Keccak256Hash([]byte("gaiko2-tee"))

	output, err := signer.SignHash(hash)
	if err != nil {
		t.Fatalf("sign hash: %v", err)
	}
	if len(output.Quote) != 0 {
		t.Fatalf("expected prove path to omit quote, got %x", output.Quote)
	}
	if output.InstanceID != 0x12345678 {
		t.Fatalf("unexpected instance id: %x", output.InstanceID)
	}
	if output.InstanceAddress != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatalf("unexpected instance address: %s", output.InstanceAddress.Hex())
	}
}

func TestTEEProofSignerAcceptsConfiguredInstanceIDZero(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer := NewTEEProofSigner(privateKey, 0)
	hash := crypto.Keccak256Hash([]byte("gaiko2-tee-zero-instance"))

	output, err := signer.SignHash(hash)
	if err != nil {
		t.Fatalf("sign hash: %v", err)
	}
	if output.InstanceID != 0 {
		t.Fatalf("unexpected instance id: %x", output.InstanceID)
	}
}

func TestNewConfiguredReplayServiceRejectsEmptyMode(t *testing.T) {
	_, err := NewConfiguredReplayService(ServiceConfig{}, nil)
	if !errors.Is(err, ErrProvingModeRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredReplayServiceAcceptsExplicitNativeMode(t *testing.T) {
	if _, err := NewConfiguredReplayService(ServiceConfig{Mode: ProvingModeNative}, nil); err != nil {
		t.Fatalf("explicit native mode: %v", err)
	}
}

func TestNewConfiguredReplayServiceRejectsUnknownMode(t *testing.T) {
	_, err := NewConfiguredReplayService(ServiceConfig{
		Mode: "wat",
	}, nil)
	if err == nil || err.Error() != `unsupported proving mode "wat"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredReplayServiceTEERequiresBootstrappedKeyAtStartup(t *testing.T) {
	prev := newTEEProviderFn
	t.Cleanup(func() {
		newTEEProviderFn = prev
	})

	newTEEProviderFn = func(tee.Config) (tee.Provider, error) {
		return &fakeTEEProvider{loadErr: fs.ErrNotExist}, nil
	}

	_, err := NewConfiguredReplayService(ServiceConfig{
		Mode:       ProvingModeTEE,
		TeeType:    tee.TypeEGo,
		SecretDir:  t.TempDir(),
		InstanceID: 0x12345678,
	}, nil)
	if err == nil || err.Error() != "tee bootstrap required: file does not exist" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredReplayServiceTEERequiresInstanceIDAtStartup(t *testing.T) {
	prev := newTEEProviderFn
	t.Cleanup(func() {
		newTEEProviderFn = prev
	})

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	newTEEProviderFn = func(tee.Config) (tee.Provider, error) {
		return &fakeTEEProvider{privateKey: privateKey}, nil
	}

	_, err = NewConfiguredReplayService(ServiceConfig{
		Mode:      ProvingModeTEE,
		TeeType:   tee.TypeEGo,
		SecretDir: t.TempDir(),
	}, nil)
	if err == nil || err.Error() != "tee proving requires GAIKO2_INSTANCE_ID or a registered GAIKO2_FORK mapping" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewConfiguredReplayServiceTEELoadsBootstrappedKeyAtStartup(t *testing.T) {
	prev := newTEEProviderFn
	t.Cleanup(func() {
		newTEEProviderFn = prev
	})

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := &fakeTEEProvider{privateKey: privateKey}
	newTEEProviderFn = func(tee.Config) (tee.Provider, error) {
		return provider, nil
	}

	_, err = NewConfiguredReplayService(ServiceConfig{
		Mode:                 ProvingModeTEE,
		TeeType:              tee.TypeEGo,
		SecretDir:            t.TempDir(),
		InstanceID:           0x12345678,
		InstanceIDConfigured: true,
	}, nil)
	if err != nil {
		t.Fatalf("new configured replay service: %v", err)
	}
	if provider.loadCalls != 1 {
		t.Fatalf("expected startup key load once, got %d", provider.loadCalls)
	}
}

type fakeTEEProvider struct {
	privateKey *ecdsa.PrivateKey
	quote      []byte
	loadErr    error
	loadCalls  int
	quoteCalls int
}

func (f *fakeTEEProvider) LoadQuote(instance common.Address) (tee.Quote, error) {
	f.quoteCalls++
	return tee.StaticQuote(f.quote), nil
}

func (f *fakeTEEProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.privateKey == nil {
		return nil, errors.New("missing key")
	}
	return f.privateKey, nil
}

func (f *fakeTEEProvider) SavePrivateKey(*ecdsa.PrivateKey, bool) error {
	return nil
}

func (f *fakeTEEProvider) HasPrivateKey() (bool, error) {
	return f.privateKey != nil, nil
}

func normalizeRecoveryID(sig [65]byte) []byte {
	out := sig[:]
	out[64] -= 27
	return out
}
