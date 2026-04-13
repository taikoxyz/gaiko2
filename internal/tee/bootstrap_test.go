package tee

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBootstrapDataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := BootstrapData{
		PublicKey:       []byte{0x04, 0x01, 0x02, 0x03},
		InstanceAddress: common.HexToAddress("0x0000777735367b36bc9b61c50022d9d0700db4ec"),
		Quote:           []byte{0xca, 0xfe, 0xba, 0xbe},
	}

	if err := SaveBootstrapData(dir, want); err != nil {
		t.Fatalf("save bootstrap data: %v", err)
	}

	got, err := LoadBootstrapData(dir)
	if err != nil {
		t.Fatalf("load bootstrap data: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bootstrap data mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRegisteredForksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := RegisteredForks{
		"shasta": 3131899904,
		"uzen":   42,
	}

	if err := SaveRegisteredForks(dir, want); err != nil {
		t.Fatalf("save registered forks: %v", err)
	}

	got, err := LoadRegisteredForks(dir)
	if err != nil {
		t.Fatalf("load registered forks: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered forks mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
