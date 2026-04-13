package prover

import (
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

const (
	ProvingModeNative = "native"
	ProvingModeTEE    = "tee"
)

type ServiceConfig struct {
	Mode       string
	TeeType    string
	SecretDir  string
	InstanceID uint32
}

type ProofSigner interface {
	SignHash(common.Hash) (SignerOutput, error)
}

type SignerOutput struct {
	Signature       [65]byte
	Quote           []byte
	PublicKey       []byte
	InstanceAddress common.Address
	InstanceID      uint32
}

type NativeProofSigner struct {
	instanceID uint32
}

type TEEProofSigner struct {
	provider   tee.Provider
	instanceID uint32
}

func NewNativeProofSigner(instanceID uint32) *NativeProofSigner {
	return &NativeProofSigner{instanceID: defaultInstanceID(instanceID)}
}

func NewTEEProofSigner(provider tee.Provider, instanceID uint32) *TEEProofSigner {
	return &TEEProofSigner{
		provider:   provider,
		instanceID: defaultInstanceID(instanceID),
	}
}

func NewConfiguredReplayService(cfg ServiceConfig, runner Runner) (ReplayService, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = ProvingModeNative
	}

	var signer ProofSigner
	switch mode {
	case ProvingModeNative:
		signer = NewNativeProofSigner(cfg.InstanceID)
	case ProvingModeTEE:
		provider, err := tee.NewProvider(tee.Config{
			Type:      cfg.TeeType,
			SecretDir: cfg.SecretDir,
		})
		if err != nil {
			return ReplayService{}, err
		}
		signer = NewTEEProofSigner(provider, cfg.InstanceID)
	default:
		return ReplayService{}, fmt.Errorf("unsupported proving mode %q", cfg.Mode)
	}

	return newReplayService(runner, signer), nil
}

func (s *NativeProofSigner) SignHash(hash common.Hash) (SignerOutput, error) {
	privateKey, err := crypto.HexToECDSA(nativeProofPrivateKey)
	if err != nil {
		return SignerOutput{}, fmt.Errorf("load native proof private key: %w", err)
	}
	return buildSignerOutput(hash, privateKey, nil, s.instanceID)
}

func (s *TEEProofSigner) SignHash(hash common.Hash) (SignerOutput, error) {
	privateKey, err := s.loadOrCreatePrivateKey()
	if err != nil {
		return SignerOutput{}, err
	}

	instanceAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	quote, err := s.provider.LoadQuote(instanceAddress)
	if err != nil {
		return SignerOutput{}, fmt.Errorf("load tee quote: %w", err)
	}

	return buildSignerOutput(hash, privateKey, quote.Bytes(), s.instanceID)
}

func buildProofResult(inputHash common.Hash, signer ProofSigner) (protocol.ProofResult, error) {
	output, err := signer.SignHash(inputHash)
	if err != nil {
		return protocol.ProofResult{}, err
	}

	proof := prefixedHex(encodeOneshotProof(output.InstanceID, output.InstanceAddress, output.Signature))
	publicKey := prefixedHex(output.PublicKey)
	instanceAddress := output.InstanceAddress.Hex()

	result := protocol.ProofResult{
		Proof:           &proof,
		PublicKey:       &publicKey,
		InstanceAddress: &instanceAddress,
		Input:           inputHash.Hex(),
	}
	if len(output.Quote) > 0 {
		quote := prefixedHex(output.Quote)
		result.Quote = &quote
	}
	return result, nil
}

func buildSignerOutput(
	hash common.Hash,
	privateKey *ecdsa.PrivateKey,
	quote []byte,
	instanceID uint32,
) (SignerOutput, error) {
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return SignerOutput{}, fmt.Errorf("sign input hash: %w", err)
	}
	signature[64] += 27

	var signed [65]byte
	copy(signed[:], signature)

	return SignerOutput{
		Signature:       signed,
		Quote:           append([]byte(nil), quote...),
		PublicKey:       crypto.FromECDSAPub(&privateKey.PublicKey),
		InstanceAddress: crypto.PubkeyToAddress(privateKey.PublicKey),
		InstanceID:      defaultInstanceID(instanceID),
	}, nil
}

func encodeOneshotProof(instanceID uint32, instanceAddress common.Address, signature [65]byte) []byte {
	proofBytes := make([]byte, 0, 4+common.AddressLength+len(signature))
	var encodedInstanceID [4]byte
	binary.BigEndian.PutUint32(encodedInstanceID[:], defaultInstanceID(instanceID))
	proofBytes = append(proofBytes, encodedInstanceID[:]...)
	proofBytes = append(proofBytes, instanceAddress.Bytes()...)
	proofBytes = append(proofBytes, signature[:]...)
	return proofBytes
}

func defaultInstanceID(instanceID uint32) uint32 {
	if instanceID == 0 {
		return shastaNativeMockInstance
	}
	return instanceID
}

func (s *TEEProofSigner) loadOrCreatePrivateKey() (*ecdsa.PrivateKey, error) {
	privateKey, err := s.provider.LoadPrivateKey()
	if err == nil {
		return privateKey, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load tee private key: %w", err)
	}

	privateKey, err = crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate tee private key: %w", err)
	}
	if err := s.provider.SavePrivateKey(privateKey); err != nil {
		return nil, fmt.Errorf("save tee private key: %w", err)
	}
	return privateKey, nil
}
