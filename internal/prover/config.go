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
	envTDXSocket   = "GAIKO2_TDXS_SOCKET"
)

func ServiceConfigFromEnv() (ServiceConfig, error) {
	cfg := ServiceConfig{
		Mode:      strings.TrimSpace(os.Getenv(envProvingMode)),
		TeeType:   strings.TrimSpace(os.Getenv(envTEEType)),
		SecretDir: envOrDefault(envSecretDir, tee.DefaultSecretDir()),
		ConfigDir: envOrDefault(envConfigDir, tee.DefaultConfigDir()),
		Fork:      strings.TrimSpace(os.Getenv(envFork)),
		TDXSocket: envOrDefault(envTDXSocket, tee.DefaultTDXSocket),
	}

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
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
