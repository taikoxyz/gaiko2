package prover

import (
	"os"
	"testing"

	"github.com/taikoxyz/gaiko2/internal/tee"
)

func TestServiceConfigFromEnvLoadsRegisteredInstanceIDForFork(t *testing.T) {
	configDir := t.TempDir()
	if err := tee.SaveRegisteredForks(configDir, tee.RegisteredForks{"shasta": 3131899904}); err != nil {
		t.Fatalf("save registered forks: %v", err)
	}

	setenv(t, envProvingMode, ProvingModeTEE)
	setenv(t, envTEEType, tee.TypeEGo)
	setenv(t, envSecretDir, t.TempDir())
	setenv(t, envConfigDir, configDir)
	setenv(t, envFork, "shasta")
	t.Cleanup(func() {
		_ = os.Unsetenv(envInstanceID)
	})
	_ = os.Unsetenv(envInstanceID)

	cfg, err := ServiceConfigFromEnv()
	if err != nil {
		t.Fatalf("service config from env: %v", err)
	}
	if cfg.InstanceID != 3131899904 {
		t.Fatalf("unexpected instance id: %d", cfg.InstanceID)
	}
}

func TestServiceConfigFromEnvRejectsUnknownRegisteredFork(t *testing.T) {
	configDir := t.TempDir()
	if err := tee.SaveRegisteredForks(configDir, tee.RegisteredForks{"shasta": 3131899904}); err != nil {
		t.Fatalf("save registered forks: %v", err)
	}

	setenv(t, envConfigDir, configDir)
	setenv(t, envFork, "uzen")
	t.Cleanup(func() {
		_ = os.Unsetenv(envInstanceID)
	})
	_ = os.Unsetenv(envInstanceID)

	_, err := ServiceConfigFromEnv()
	if err == nil || err.Error() != `registered instance id for fork "uzen" not found` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceConfigFromEnvLoadsTDXGethConfig(t *testing.T) {
	setenv(t, envProvingMode, ProvingModeTDXGeth)
	setenv(t, envTEEType, tee.TypeTDX)
	setenv(t, envTDXSocket, "/tmp/tdxs.sock")
	setenv(t, envL2RPCURL, "http://127.0.0.1:9545")
	setenv(t, envAllowRemoteL2RPC, "true")

	cfg, err := ServiceConfigFromEnv()
	if err != nil {
		t.Fatalf("service config from env: %v", err)
	}
	if cfg.Mode != ProvingModeTDXGeth {
		t.Fatalf("unexpected mode: %s", cfg.Mode)
	}
	if cfg.TeeType != tee.TypeTDX {
		t.Fatalf("unexpected tee type: %s", cfg.TeeType)
	}
	if cfg.TDXSocket != "/tmp/tdxs.sock" {
		t.Fatalf("unexpected tdx socket: %s", cfg.TDXSocket)
	}
	if cfg.L2RPCURL != "http://127.0.0.1:9545" {
		t.Fatalf("unexpected l2 rpc url: %s", cfg.L2RPCURL)
	}
	if !cfg.AllowRemoteL2RPC {
		t.Fatalf("expected remote l2 rpc override")
	}
}

func setenv(t *testing.T, key, value string) {
	t.Helper()
	prev, ok := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, prev)
			return
		}
		_ = os.Unsetenv(key)
	})
}
