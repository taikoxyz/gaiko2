package prover

import (
	"context"
	"errors"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

var ErrNotImplemented = errors.New("gaiko2 proving is not implemented yet")

// ErrAggregateDisabled is returned by Aggregate when the /prove/shasta-aggregate
// endpoint is disabled. In native mode the endpoint signs the on-chain digest
// with the published mock key without executing any blocks, so it is only served
// in TEE mode or when native mode is explicitly opted into dev mode
// (GAIKO2_DEV_MODE).
var ErrAggregateDisabled = errors.New("aggregate proving is disabled in native mode without GAIKO2_DEV_MODE")

type Service interface {
	Prove(ctx context.Context, req *ValidatedRequest) (protocol.ProofResult, error)
	Aggregate(ctx context.Context, req *ValidatedAggregateRequest) (protocol.ProofResult, error)
}

type StubService struct{}

func (StubService) Prove(context.Context, *ValidatedRequest) (protocol.ProofResult, error) {
	return protocol.ProofResult{}, ErrNotImplemented
}

func (StubService) Aggregate(context.Context, *ValidatedAggregateRequest) (protocol.ProofResult, error) {
	return protocol.ProofResult{}, ErrNotImplemented
}
