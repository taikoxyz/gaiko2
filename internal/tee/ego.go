package tee

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/edgelesssys/ego/ecrypto"
	"github.com/edgelesssys/ego/enclave"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type EGoProvider struct {
	secretDir string
}

func NewEGoProvider(secretDir string) *EGoProvider {
	return &EGoProvider{secretDir: secretDir}
}

func (p *EGoProvider) LoadQuote(instance common.Address) (Quote, error) {
	report, err := enclave.GetRemoteReport(instance.Bytes())
	if err != nil {
		return nil, err
	}
	// The first 16 bytes are the quote nonce.
	if len(report) < 16 {
		return nil, fmt.Errorf("unexpected report length: %d", len(report))
	}
	return StaticQuote(report[16:]), nil
}

func (p *EGoProvider) HasPrivateKey() (bool, error) {
	_, err := os.Stat(filepath.Join(p.secretDir, privateKeyFilename))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (p *EGoProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	sealedText, err := os.ReadFile(filepath.Join(p.secretDir, privateKeyFilename))
	if err != nil {
		return nil, err
	}

	plainText, err := ecrypto.Unseal(sealedText, nil)
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(plainText)
}

func (p *EGoProvider) SavePrivateKey(privKey *ecdsa.PrivateKey) error {
	plainText := crypto.FromECDSA(privKey)
	sealedText, err := ecrypto.SealWithUniqueKey(plainText, nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.secretDir, 0o700); err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(p.secretDir, privateKeyFilename), sealedText, 0o600)
}
