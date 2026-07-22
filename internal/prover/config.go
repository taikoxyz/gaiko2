package prover

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/taikoxyz/gaiko2/internal/tee"
)

const (
	envProvingMode = "GAIKO2_PROVING_MODE"
	envTEEType     = "GAIKO2_TEE_TYPE"
	envSecretDir   = "GAIKO2_SECRET_DIR"
	envConfigDir   = "GAIKO2_CONFIG_DIR"
	envFork        = "GAIKO2_FORK"
	envInstanceID  = "GAIKO2_INSTANCE_ID"
)

func ServiceConfigFromEnv() (ServiceConfig, error) {
	cfg := ServiceConfig{
		Mode:      strings.TrimSpace(os.Getenv(envProvingMode)),
		TeeType:   strings.TrimSpace(os.Getenv(envTEEType)),
		SecretDir: envOrDefault(envSecretDir, tee.DefaultSecretDir()),
		ConfigDir: envOrDefault(envConfigDir, tee.DefaultConfigDir()),
		Fork:      strings.TrimSpace(os.Getenv(envFork)),
	}
	mode, err := normalizeProvingMode(cfg.Mode)
	if err != nil {
		return ServiceConfig{}, err
	}
	cfg.Mode = mode

	instanceID := strings.TrimSpace(os.Getenv(envInstanceID))
	if instanceID == "" && cfg.Fork != "" {
		registered, err := tee.LoadRegisteredForks(cfg.ConfigDir)
		if err != nil {
			return ServiceConfig{}, fmt.Errorf("load registered instance ids: %w", err)
		}
		resolved, ok := registered[cfg.Fork]
		if !ok {
			return ServiceConfig{}, fmt.Errorf("registered instance id for fork %q not found", cfg.Fork)
		}
		if resolved > math.MaxUint32 {
			return ServiceConfig{}, fmt.Errorf("registered instance id for fork %q overflows uint32", cfg.Fork)
		}
		cfg.InstanceID = uint32(resolved)
		cfg.InstanceIDConfigured = true
		return cfg, nil
	}
	if instanceID == "" {
		return cfg, nil
	}

	parsed, err := strconv.ParseUint(instanceID, 0, 32)
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("parse %s: %w", envInstanceID, err)
	}
	cfg.InstanceID = uint32(parsed)
	cfg.InstanceIDConfigured = true
	return cfg, nil
}

func normalizeProvingMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "", fmt.Errorf(
			"%s must be set to %q or %q",
			envProvingMode,
			ProvingModeNative,
			ProvingModeTEE,
		)
	}
	return mode, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
