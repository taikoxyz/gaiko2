package prover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

const sharedFixtureName = "shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json"

func TestGethRunnerRejectsDeferredStateErrors(t *testing.T) {
	req := loadSharedShastaFixture(t)
	validated, err := ValidateRequest(req)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}
	config, err := chainConfigFor(validated.Carry.ChainID)
	if err != nil {
		t.Fatalf("chain config: %v", err)
	}

	decodeFirstReplay := func(t *testing.T) (*gethtypes.Block, *ReplayWitness) {
		t.Helper()
		block, witness, err := decodeReplayBlock(validated.Request.Payload.Blocks[0])
		if err != nil {
			t.Fatalf("decode first replay block: %v", err)
		}
		return block, witness
	}

	t.Run("after block processing", func(t *testing.T) {
		block, witness := decodeFirstReplay(t)
		witness.Witness = witness.Witness.Copy()
		missingNodeHash := common.HexToHash("0xce7fe85bcf0a6f5b9b311309e0c4af9daea7879269315b0c49a1fffb6dab38ea")
		removed := false
		for node := range witness.Witness.State {
			if crypto.Keccak256Hash([]byte(node)) == missingNodeHash {
				delete(witness.Witness.State, node)
				removed = true
				break
			}
		}
		if !removed {
			t.Fatalf("fixture no longer contains state node %s", missingNodeHash)
		}

		_, err := (GethRunner{}).Execute(
			context.Background(),
			config,
			blockForStatelessExecution(block),
			witness,
		)
		var missingNode *trie.MissingNodeError
		if !errors.As(err, &missingNode) {
			t.Fatalf("expected wrapped missing trie node error, got %v", err)
		}
		if missingNode.NodeHash != missingNodeHash {
			t.Fatalf("unexpected missing trie node: got %s want %s", missingNode.NodeHash, missingNodeHash)
		}
		if !strings.Contains(err.Error(), "witness state error after block processing") {
			t.Fatalf("expected block-processing phase context, got %v", err)
		}
	})

	t.Run("after intermediate root", func(t *testing.T) {
		block, witness := decodeFirstReplay(t)
		sentinel := errors.New("deferred state error after intermediate root")
		runner := GethRunner{stateErrorCheck: func(source replayStateErrorSource, phase string) error {
			if phase != "after intermediate root" {
				return replayStateError(source, phase)
			}
			db, ok := source.(*state.StateDB)
			if !ok {
				return fmt.Errorf("unexpected replay state source %T", source)
			}
			if db.GetTrie() == nil || db.GetTrie().Hash() != block.Root() {
				return fmt.Errorf("post-root state check ran before IntermediateRoot")
			}
			return replayStateError(replayStateErrorStub{err: sentinel}, phase)
		}}

		_, err := runner.Execute(
			context.Background(),
			config,
			blockForStatelessExecution(block),
			witness,
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected wrapped post-root state error, got %v", err)
		}
		if !strings.Contains(err.Error(), "witness state error after intermediate root") {
			t.Fatalf("expected intermediate-root phase context, got %v", err)
		}
	})
}

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
