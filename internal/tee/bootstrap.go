package tee

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

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

// ErrPrivateKeyUnavailable identifies an existing sealed key that cannot be
// loaded, including keys sealed to a different enclave identity.
var ErrPrivateKeyUnavailable = errors.New("tee private key is unavailable")

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
	data, err := bootstrapData(provider, privateKey)
	if err != nil {
		return BootstrapData{}, err
	}

	if err := provider.SavePrivateKey(privateKey, force); err != nil {
		return BootstrapData{}, fmt.Errorf("save tee private key: %w", err)
	}

	return data, nil
}

func bootstrapData(provider Provider, privateKey *ecdsa.PrivateKey) (BootstrapData, error) {
	instanceAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	quote, err := provider.LoadQuote(instanceAddress)
	if err != nil {
		return BootstrapData{}, fmt.Errorf("load tee quote: %w", err)
	}
	return BootstrapData{
		PublicKey:       hexutil.Bytes(crypto.FromECDSAPub(&privateKey.PublicKey)),
		InstanceAddress: instanceAddress,
		Quote:           hexutil.Bytes(quote.Bytes()),
	}, nil
}

func BootstrapDataForExistingKey(provider Provider, configDir string) (BootstrapData, bool, error) {
	privateKey, err := provider.LoadPrivateKey()
	if err != nil {
		return BootstrapData{}, false, fmt.Errorf(
			"%w: load existing tee private key: %w",
			ErrPrivateKeyUnavailable,
			err,
		)
	}
	publicKey := crypto.FromECDSAPub(&privateKey.PublicKey)
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	existing, loadErr := LoadBootstrapData(configDir)
	if loadErr == nil &&
		existing.InstanceAddress == address &&
		bytes.Equal(existing.PublicKey, publicKey) &&
		len(existing.Quote) > 0 {
		return existing, true, nil
	}
	if loadErr != nil {
		var pathErr *os.PathError
		if errors.As(loadErr, &pathErr) && !errors.Is(loadErr, fs.ErrNotExist) {
			return BootstrapData{}, false, fmt.Errorf("load existing bootstrap data: %w", loadErr)
		}
	}

	data, err := bootstrapData(provider, privateKey)
	if err != nil {
		return BootstrapData{}, false, fmt.Errorf("rebuild bootstrap data: %w", err)
	}
	return data, false, nil
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
	if err := ensureDirectory(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return atomicWriteFile(path, encoded, 0o644, true)
}

// ensureDirectory creates missing path components and makes each new directory
// entry durable before returning. os.MkdirAll alone does not fsync the parent
// directories that name newly created children.
func ensureDirectory(path string, perm os.FileMode) error {
	path = filepath.Clean(path)
	missing := make([]string, 0)
	current := path
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return &os.PathError{Op: "mkdir", Path: current, Err: syscall.ENOTDIR}
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return err
		}
		current = parent
	}

	for i := len(missing) - 1; i >= 0; i-- {
		dirPath := missing[i]
		created := false
		if err := os.Mkdir(dirPath, perm); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, statErr := os.Stat(dirPath)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() {
				return &os.PathError{Op: "mkdir", Path: dirPath, Err: syscall.ENOTDIR}
			}
		} else {
			created = true
		}

		if created {
			if err := os.Chmod(dirPath, perm); err != nil {
				return err
			}
		}
	}

	// Sync the full chain even when every component already existed when it
	// was observed. Another process may have just created one of those entries
	// without making its parent durable yet.
	return syncDirectoryAncestors(path)
}

func syncDirectoryAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		if err := syncDirectoryFn(current); err != nil {
			// EGo hostfs mounts can expose the target directory without its
			// virtual parent directories. Once the target was synced, ENOENT
			// marks that mount boundary rather than a failed durable write.
			if current != path && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// atomicWriteFile writes via a temp file in the target directory and installs
// it atomically, so a crash mid-write can never leave a truncated key or torn
// JSON behind. When overwrite is false, installation fails if path exists.
func atomicWriteFile(path string, data []byte, perm os.FileMode, overwrite bool) error {
	dirPath := filepath.Dir(path)
	tmp, err := os.CreateTemp(dirPath, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if overwrite {
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
		cleanup = false
	} else {
		if err := os.Link(tmpName, path); err != nil {
			return err
		}
		if err := os.Remove(tmpName); err != nil {
			return err
		}
		cleanup = false
	}
	return syncDirectoryFn(dirPath)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

var syncDirectoryFn = syncDirectory

func readJSON(path string, value any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(contents, value)
}
