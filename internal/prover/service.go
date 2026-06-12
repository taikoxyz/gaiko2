package prover

import (
	"context"
	"errors"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

var ErrNotImplemented = errors.New("gaiko2 proving is not implemented yet")

type Service interface {
	Prove(ctx context.Context, req *ValidatedRequest) (protocol.ProofResult, error)
	Aggregate(ctx context.Context, req *ValidatedAggregateRequest) (protocol.ProofResult, error)
	DirectAggregate(ctx context.Context, req *ValidatedDirectAggregateRequest) (protocol.ProofResult, error)
}

type StubService struct{}

func (StubService) Prove(context.Context, *ValidatedRequest) (protocol.ProofResult, error) {
	return protocol.ProofResult{}, ErrNotImplemented
}

func (StubService) Aggregate(context.Context, *ValidatedAggregateRequest) (protocol.ProofResult, error) {
	return protocol.ProofResult{}, ErrNotImplemented
}

func (StubService) DirectAggregate(
	context.Context,
	*ValidatedDirectAggregateRequest,
) (protocol.ProofResult, error) {
	return protocol.ProofResult{}, ErrNotImplemented
}
