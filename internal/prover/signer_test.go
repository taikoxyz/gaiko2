package prover

import (
	"crypto/ecdsa"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

func TestNativeProofSignerMatchesGoldenTouchAccount(t *testing.T) {
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
}

func TestTEEProofSignerReturnsQuote(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := &fakeTEEProvider{
		privateKey: privateKey,
		quote:      []byte{0xca, 0xfe},
	}
	signer := NewTEEProofSigner(provider, 0x12345678)
	hash := crypto.Keccak256Hash([]byte("gaiko2-tee"))

	output, err := signer.SignHash(hash)
	if err != nil {
		t.Fatalf("sign hash: %v", err)
	}
	if provider.loadCalls != 1 {
		t.Fatalf("expected key load once, got %d", provider.loadCalls)
	}
	if provider.quoteCalls != 1 {
		t.Fatalf("expected quote load once, got %d", provider.quoteCalls)
	}
	if output.InstanceID != 0x12345678 {
		t.Fatalf("unexpected instance id: %x", output.InstanceID)
	}
	if string(output.Quote) != string([]byte{0xca, 0xfe}) {
		t.Fatalf("unexpected quote: %x", output.Quote)
	}
	if output.InstanceAddress != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatalf("unexpected instance address: %s", output.InstanceAddress.Hex())
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

type fakeTEEProvider struct {
	privateKey *ecdsa.PrivateKey
	quote      []byte
	loadCalls  int
	quoteCalls int
}

func (f *fakeTEEProvider) LoadQuote(instance common.Address) (tee.Quote, error) {
	f.quoteCalls++
	return tee.StaticQuote(f.quote), nil
}

func (f *fakeTEEProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	f.loadCalls++
	if f.privateKey == nil {
		return nil, errors.New("missing key")
	}
	return f.privateKey, nil
}

func (f *fakeTEEProvider) SavePrivateKey(*ecdsa.PrivateKey) error {
	return nil
}

func normalizeRecoveryID(sig [65]byte) []byte {
	out := sig[:]
	out[64] -= 27
	return out
}
