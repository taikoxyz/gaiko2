package prover

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"

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
	Mode                 string
	TeeType              string
	SecretDir            string
	ConfigDir            string
	Fork                 string
	InstanceID           uint32
	InstanceIDConfigured bool
}

type ProofSigner interface {
	SignHash(common.Hash) (SignerOutput, error)
	Identity() (SignerIdentity, error)
}

type SignerIdentity struct {
	PublicKey       []byte
	InstanceAddress common.Address
	InstanceID      uint32
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
	privateKey           *ecdsa.PrivateKey
	instanceID           uint32
	instanceIDConfigured bool
}

var newTEEProviderFn = tee.NewProvider

func NewNativeProofSigner(instanceID uint32) *NativeProofSigner {
	return &NativeProofSigner{instanceID: defaultInstanceID(instanceID)}
}

func NewTEEProofSigner(privateKey *ecdsa.PrivateKey, instanceID uint32) *TEEProofSigner {
	return &TEEProofSigner{
		privateKey:           privateKey,
		instanceID:           instanceID,
		instanceIDConfigured: true,
	}
}

func newTEEProofSignerFromConfig(privateKey *ecdsa.PrivateKey, cfg ServiceConfig) *TEEProofSigner {
	return &TEEProofSigner{
		privateKey:           privateKey,
		instanceID:           cfg.InstanceID,
		instanceIDConfigured: cfg.InstanceIDConfigured,
	}
}

func NewConfiguredReplayService(cfg ServiceConfig, runner Runner) (ReplayService, error) {
	mode, err := normalizeProvingMode(cfg.Mode)
	if err != nil {
		return ReplayService{}, err
	}

	var signer ProofSigner
	switch mode {
	case ProvingModeNative:
		signer = NewNativeProofSigner(cfg.InstanceID)
	case ProvingModeTEE:
		provider, err := newTEEProviderFn(tee.Config{
			Type:      cfg.TeeType,
			SecretDir: cfg.SecretDir,
		})
		if err != nil {
			return ReplayService{}, err
		}
		privateKey, err := provider.LoadPrivateKey()
		if err != nil {
			return ReplayService{}, fmt.Errorf("tee bootstrap required: %w", err)
		}
		if !cfg.InstanceIDConfigured {
			return ReplayService{}, fmt.Errorf("tee proving requires %s or a registered %s mapping", envInstanceID, envFork)
		}
		signer = newTEEProofSignerFromConfig(privateKey, cfg)
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

func (s *NativeProofSigner) Identity() (SignerIdentity, error) {
	privateKey, err := crypto.HexToECDSA(nativeProofPrivateKey)
	if err != nil {
		return SignerIdentity{}, fmt.Errorf("load native proof private key: %w", err)
	}
	return signerIdentity(privateKey, s.instanceID), nil
}

func (s *TEEProofSigner) SignHash(hash common.Hash) (SignerOutput, error) {
	if !s.instanceIDConfigured {
		return SignerOutput{}, fmt.Errorf("tee proving requires %s or a registered %s mapping", envInstanceID, envFork)
	}
	return buildSignerOutput(hash, s.privateKey, nil, s.instanceID)
}

func (s *TEEProofSigner) Identity() (SignerIdentity, error) {
	if !s.instanceIDConfigured {
		return SignerIdentity{}, fmt.Errorf("tee proving requires %s or a registered %s mapping", envInstanceID, envFork)
	}
	return signerIdentity(s.privateKey, s.instanceID), nil
}

func buildProofResult(inputHash common.Hash, signer ProofSigner) (protocol.ProofResult, error) {
	output, err := signer.SignHash(inputHash)
	if err != nil {
		return protocol.ProofResult{}, err
	}

	return proofResultFromSignerOutput(inputHash, output), nil
}

func proofResultFromSignerOutput(inputHash common.Hash, output SignerOutput) protocol.ProofResult {
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
	return result
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
		InstanceID:      instanceID,
	}, nil
}

func signerIdentity(privateKey *ecdsa.PrivateKey, instanceID uint32) SignerIdentity {
	return SignerIdentity{
		PublicKey:       crypto.FromECDSAPub(&privateKey.PublicKey),
		InstanceAddress: crypto.PubkeyToAddress(privateKey.PublicKey),
		InstanceID:      instanceID,
	}
}

func encodeOneshotProof(instanceID uint32, instanceAddress common.Address, signature [65]byte) []byte {
	proofBytes := make([]byte, 0, 4+common.AddressLength+len(signature))
	var encodedInstanceID [4]byte
	binary.BigEndian.PutUint32(encodedInstanceID[:], instanceID)
	proofBytes = append(proofBytes, encodedInstanceID[:]...)
	proofBytes = append(proofBytes, instanceAddress.Bytes()...)
	proofBytes = append(proofBytes, signature[:]...)
	return proofBytes
}

func decodeOneshotProof(proofBytes []byte) (uint32, common.Address, []byte, error) {
	if len(proofBytes) != 4+common.AddressLength+65 {
		return 0, common.Address{}, nil, fmt.Errorf(
			"proof length mismatch: got %d expected %d",
			len(proofBytes),
			4+common.AddressLength+65,
		)
	}
	instanceID := binary.BigEndian.Uint32(proofBytes[:4])
	instanceAddress := common.BytesToAddress(proofBytes[4 : 4+common.AddressLength])
	signature := append([]byte(nil), proofBytes[4+common.AddressLength:]...)
	return instanceID, instanceAddress, signature, nil
}

func defaultInstanceID(instanceID uint32) uint32 {
	if instanceID == 0 {
		return shastaNativeMockInstance
	}
	return instanceID
}
