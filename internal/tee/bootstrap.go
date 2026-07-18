package tee

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	bootstrapInfoFilename  = "bootstrap.gaiko2.json"
	registeredInfoFilename = "registered.gaiko2.json"
)

type BootstrapData struct {
	PublicKey       hexutil.Bytes  `json:"public_key"`
	InstanceAddress common.Address `json:"new_instance"`
	Quote           hexutil.Bytes  `json:"quote"`
}

type RegisteredForks map[string]uint64

// ErrPrivateKeyExists is returned by Bootstrap when a sealed key is already
// present and force was not requested.
var ErrPrivateKeyExists = errors.New("tee private key already exists")

func Bootstrap(provider Provider, force bool) (BootstrapData, error) {
	if !force {
		exists, err := provider.HasPrivateKey()
		if err != nil {
			return BootstrapData{}, fmt.Errorf("check existing tee private key: %w", err)
		}
		if exists {
			return BootstrapData{}, ErrPrivateKeyExists
		}
	}

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return BootstrapData{}, fmt.Errorf("generate tee private key: %w", err)
	}

	// Fetch the quote before persisting anything so a quote failure cannot
	// leave a fresh key on disk that no saved bootstrap data describes.
	instanceAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	quote, err := provider.LoadQuote(instanceAddress)
	if err != nil {
		return BootstrapData{}, fmt.Errorf("load tee quote: %w", err)
	}

	if err := provider.SavePrivateKey(privateKey); err != nil {
		return BootstrapData{}, fmt.Errorf("save tee private key: %w", err)
	}

	return BootstrapData{
		PublicKey:       hexutil.Bytes(crypto.FromECDSAPub(&privateKey.PublicKey)),
		InstanceAddress: instanceAddress,
		Quote:           hexutil.Bytes(quote.Bytes()),
	}, nil
}

func Check(provider Provider) error {
	_, err := provider.LoadPrivateKey()
	return err
}

func SaveBootstrapData(configDir string, data BootstrapData) error {
	return writeJSON(filepath.Join(configDir, bootstrapInfoFilename), data)
}

func LoadBootstrapData(configDir string) (BootstrapData, error) {
	var data BootstrapData
	if err := readJSON(filepath.Join(configDir, bootstrapInfoFilename), &data); err != nil {
		return BootstrapData{}, err
	}
	return data, nil
}

func SaveRegisteredForks(configDir string, forks RegisteredForks) error {
	if forks == nil {
		forks = RegisteredForks{}
	}
	return writeJSON(filepath.Join(configDir, registeredInfoFilename), forks)
}

func LoadRegisteredForks(configDir string) (RegisteredForks, error) {
	var forks RegisteredForks
	if err := readJSON(filepath.Join(configDir, registeredInfoFilename), &forks); err != nil {
		return nil, err
	}
	if forks == nil {
		forks = RegisteredForks{}
	}
	return forks, nil
}

func DefaultConfigDir() string {
	return defaultDir("config")
}

func DefaultSecretDir() string {
	return defaultDir("secrets")
}

func defaultDir(kind string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return filepath.Join(".", kind)
	}
	return filepath.Join(homeDir, ".config", "gaiko2", kind)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return atomicWriteFile(path, encoded, 0o644)
}

// atomicWriteFile writes via a temp file in the target directory and renames
// it into place, so a crash mid-write can never leave a truncated key or
// torn JSON behind.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	writeErr := func() error {
		if _, err := tmp.Write(data); err != nil {
			return err
		}
		if err := tmp.Chmod(perm); err != nil {
			return err
		}
		if err := tmp.Sync(); err != nil {
			return err
		}
		return tmp.Close()
	}()
	if writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return writeErr
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func readJSON(path string, value any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(contents, value)
}
