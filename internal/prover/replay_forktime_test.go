package prover

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

func TestEnableUnzenForksFromPinsNilForkTimes(t *testing.T) {
	const ts = uint64(1786021200)
	cfg := &params.ChainConfig{}
	if err := enableUnzenForksFrom(cfg, ts); err != nil {
		t.Fatalf("enable unzen forks: %v", err)
	}
	for name, got := range map[string]*uint64{
		"Unzen":  cfg.UnzenTime,
		"Cancun": cfg.CancunTime,
		"Prague": cfg.PragueTime,
		"Osaka":  cfg.OsakaTime,
	} {
		if got == nil || *got != ts {
			t.Fatalf("%s activation time not pinned to %d: %v", name, ts, got)
		}
	}
	if cfg.BlobScheduleConfig == nil {
		t.Fatal("expected blob schedule to be populated")
	}
}

func TestEnableUnzenForksFromAcceptsMatchingPreset(t *testing.T) {
	const ts = uint64(1786021200)
	preset := ts
	cfg := &params.ChainConfig{CancunTime: &preset}
	if err := enableUnzenForksFrom(cfg, ts); err != nil {
		t.Fatalf("matching preset fork time must be accepted: %v", err)
	}
}

func TestEnableUnzenForksFromRejectsConflictingPreset(t *testing.T) {
	const ts = uint64(1786021200)
	other := ts + 1
	cfg := &params.ChainConfig{CancunTime: &other}
	err := enableUnzenForksFrom(cfg, ts)
	if err == nil || !strings.Contains(err.Error(), "unexpected Cancun activation time") {
		t.Fatalf("expected conflicting Cancun activation-time rejection, got %v", err)
	}
}
