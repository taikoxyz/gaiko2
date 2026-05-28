package prover

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

type TDXGethService struct {
	headers L2HeaderSource
	signer  ProofSigner
}

func NewTDXGethService(headers L2HeaderSource, signer ProofSigner) TDXGethService {
	return TDXGethService{
		headers: headers,
		signer:  signer,
	}
}

func (s TDXGethService) Prove(
	ctx context.Context,
	req *ValidatedRequest,
) (protocol.ProofResult, error) {
	if s.headers == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth local L2 header source is not configured")
	}
	if s.signer == nil {
		return protocol.ProofResult{}, fmt.Errorf("tdxgeth signer is not configured")
	}

	if err := s.verifyLocalHeaders(ctx, req.Blocks); err != nil {
		return protocol.ProofResult{}, err
	}

	inputHash, err := hashShastaSubproofInput(req.Request.Payload.ProofCarryData)
	if err != nil {
		return protocol.ProofResult{}, err
	}
	return buildProofResult(inputHash, s.signer)
}

func (s TDXGethService) Aggregate(
	_ context.Context,
	req *ValidatedAggregateRequest,
) (protocol.ProofResult, error) {
	return aggregateWithSigner(s.signer, req)
}

func (s TDXGethService) verifyLocalHeaders(ctx context.Context, blocks []BlockView) error {
	for _, expected := range blocks {
		actual, err := s.headers.HeaderByNumber(ctx, expected.Number)
		if err != nil {
			return fmt.Errorf("fetch local L2 block %d: %w", expected.Number, err)
		}
		if actual.Number != expected.Number {
			return fmt.Errorf(
				"local L2 block number mismatch: got %d expected %d",
				actual.Number,
				expected.Number,
			)
		}
		if err := compareHash("block hash", expected.Number, actual.Hash, expected.Hash); err != nil {
			return err
		}
		if err := compareHash("parent hash", expected.Number, actual.ParentHash, expected.ParentHash); err != nil {
			return err
		}
		if err := compareHash("state root", expected.Number, actual.StateRoot, expected.StateRoot); err != nil {
			return err
		}
		if err := compareHash("receipts root", expected.Number, actual.ReceiptsRoot, expected.ReceiptsRoot); err != nil {
			return err
		}
	}
	return nil
}

func compareHash(label string, number uint64, got common.Hash, expected common.Hash) error {
	if got == expected {
		return nil
	}
	return fmt.Errorf(
		"local L2 %s mismatch for block %d: got %s expected %s",
		label,
		number,
		got.Hex(),
		expected.Hex(),
	)
}
