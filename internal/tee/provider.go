package tee

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	privateKeyFilename = "priv.gaiko2.key"
	TypeEGo            = "ego"
)

// Provider persists the enclave-managed signing key and returns an attestation quote
// bound to the proving instance address.
type Provider interface {
	LoadQuote(instance common.Address) (Quote, error)
	LoadPrivateKey() (*ecdsa.PrivateKey, error)
	SavePrivateKey(*ecdsa.PrivateKey) error
	HasPrivateKey() (bool, error)
}

type Config struct {
	Type      string
	SecretDir string
}

func NewProvider(cfg Config) (Provider, error) {
	providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
	if providerType == "" {
		providerType = TypeEGo
	}

	switch providerType {
	case TypeEGo:
		if strings.TrimSpace(cfg.SecretDir) == "" {
			return nil, fmt.Errorf("tee secret dir is required for %s provider", providerType)
		}
		return NewEGoProvider(cfg.SecretDir), nil
	default:
		return nil, fmt.Errorf("unsupported tee type %q", cfg.Type)
	}
}
