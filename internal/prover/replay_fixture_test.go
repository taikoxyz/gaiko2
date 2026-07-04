package prover

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

const sharedFixtureName = "shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json"

func TestSharedShastaFixtureMetadata(t *testing.T) {
	req := loadSharedShastaFixture(t)
	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}

	if req.Schema != protocol.ShastaRequestSchemaV1 {
		t.Fatalf("unexpected schema: %s", req.Schema)
	}
	if validated.Request.Payload.ChainID != 167000 {
		t.Fatalf("unexpected chain id: %d", validated.Request.Payload.ChainID)
	}
	if len(validated.Request.Payload.Blocks) != 192 {
		t.Fatalf("unexpected block count: %d", len(validated.Request.Payload.Blocks))
	}
	if validated.Carry.ChainID != 167000 {
		t.Fatalf("unexpected carry chain id: %d", validated.Carry.ChainID)
	}
	if validated.Blocks[0].Number != 5412225 || validated.Blocks[len(validated.Blocks)-1].Number != 5412416 {
		t.Fatalf("unexpected block span: first=%d last=%d", validated.Blocks[0].Number, validated.Blocks[len(validated.Blocks)-1].Number)
	}
}

func TestSharedShastaFixtureReplaysStateless(t *testing.T) {
	req := loadSharedShastaFixture(t)

	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := NewReplayService(nil).Prove(ctx, validated)
	if err != nil {
		t.Fatalf("replay shared fixture: %v", err)
	}
	if result.Proof == nil || *result.Proof == "" {
		t.Fatalf("expected proof payload, got %+v", result)
	}
	if result.Input == "" {
		t.Fatalf("expected input hash, got %+v", result)
	}
}

func TestSharedShastaFixtureFirstAnchorTransactionDecodes(t *testing.T) {
	req := loadSharedShastaFixture(t)
	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}

	block, _, err := decodeReplayBlock(validated.Request.Payload.Blocks[0])
	if err != nil {
		t.Fatalf("decode replay block: %v", err)
	}
	if len(block.Transactions()) != 1 {
		t.Fatalf("unexpected transaction count: %d", len(block.Transactions()))
	}

	cfg, err := chainConfigFor(validated.Request.Payload.ChainID)
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}
	tx := block.Transactions()[0]
	signer := gethtypes.MakeSigner(cfg, block.Number(), block.Time())
	sender, err := gethtypes.Sender(signer, tx)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if sender != common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec") {
		t.Fatalf("unexpected sender: %s", sender.Hex())
	}
	if tx.To() == nil || *tx.To() != common.HexToAddress("0x1670000000000000000000000000000000010001") {
		t.Fatalf("unexpected tx destination: %v", tx.To())
	}
	if tx.Gas() != 1_000_000 {
		t.Fatalf("unexpected gas limit: %d", tx.Gas())
	}
	if len(tx.Data()) < 4 || common.Bytes2Hex(tx.Data()[:4]) != "523e6854" {
		t.Fatalf("unexpected anchor selector: %x", tx.Data())
	}
}

func loadSharedShastaFixture(t *testing.T) protocol.ShastaRequest {
	t.Helper()

	data, err := os.ReadFile(sharedShastaFixturePath())
	if err != nil {
		t.Fatalf("read shared shasta fixture: %v", err)
	}

	var req protocol.ShastaRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("decode shared shasta fixture: %v", err)
	}

	return req
}

func sharedShastaFixturePath() string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	return filepath.Join(root, "testdata", sharedFixtureName)
}
