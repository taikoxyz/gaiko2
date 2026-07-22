package prover

import (
	"os"
	"testing"
)

func TestEnvFlagEnabled(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "Yes", "on", " on "} {
		setenv(t, envDevMode, value)
		if !envFlagEnabled(envDevMode) {
			t.Fatalf("expected %q to be treated as enabled", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "off", "enable"} {
		setenv(t, envDevMode, value)
		if envFlagEnabled(envDevMode) {
			t.Fatalf("expected %q to be treated as disabled", value)
		}
	}
}

func TestServiceConfigFromEnvParsesDevMode(t *testing.T) {
	_ = os.Unsetenv(envDevMode)
	cfg, err := ServiceConfigFromEnv()
	if err != nil {
		t.Fatalf("service config from env: %v", err)
	}
	if cfg.DevMode {
		t.Fatalf("expected dev mode disabled when %s is unset", envDevMode)
	}

	setenv(t, envDevMode, "1")
	cfg, err = ServiceConfigFromEnv()
	if err != nil {
		t.Fatalf("service config from env: %v", err)
	}
	if !cfg.DevMode {
		t.Fatalf("expected dev mode enabled for %s=1", envDevMode)
	}
}
