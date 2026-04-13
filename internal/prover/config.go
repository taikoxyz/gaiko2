package prover

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	envProvingMode = "GAIKO2_PROVING_MODE"
	envTEEType     = "GAIKO2_TEE_TYPE"
	envSecretDir   = "GAIKO2_SECRET_DIR"
	envInstanceID  = "GAIKO2_INSTANCE_ID"
)

func ServiceConfigFromEnv() (ServiceConfig, error) {
	cfg := ServiceConfig{
		Mode:      strings.TrimSpace(os.Getenv(envProvingMode)),
		TeeType:   strings.TrimSpace(os.Getenv(envTEEType)),
		SecretDir: strings.TrimSpace(os.Getenv(envSecretDir)),
	}

	instanceID := strings.TrimSpace(os.Getenv(envInstanceID))
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
