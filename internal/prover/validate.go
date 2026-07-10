package prover

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func ValidateRequest(req protocol.ShastaRequest) (*ValidatedRequest, error) {
	return ValidateRequestWithContext(context.Background(), req)
}

func ValidateRequestWithContext(ctx context.Context, req protocol.ShastaRequest) (*ValidatedRequest, error) {
	switch req.Schema {
	case protocol.ShastaRequestSchemaV1:
		return validateGuestInputRequest(ctx, req)
	default:
		return nil, validationError(fmt.Errorf("unsupported schema %q", req.Schema), requestLogMetadata(req))
	}
}

func validateGuestInputRequest(ctx context.Context, req protocol.ShastaRequest) (*ValidatedRequest, error) {
	metadata := requestLogMetadata(req)
	if req.Payload.GuestInput == nil {
		return nil, validationError(fmt.Errorf("request must include guest_input"), metadata)
	}

	view, err := DecodeGuestInput(*req.Payload.GuestInput)
	if err != nil {
		return nil, validationError(err, metadata)
	}
	metadata = requestLogMetadataFromView(req, view)
	if err := ValidateGuestInputCarry(view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := ValidateGuestInputBlobSources(view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := ValidateGuestInputManifestBindingWithContext(ctx, view); err != nil {
		return nil, validationError(err, metadata)
	}
	if err := validateBlockViews(view.Blocks, view.Carry); err != nil {
		return nil, validationError(err, metadata)
	}

	blocks := make([]protocol.ReplayBlock, len(view.Witnesses))
	for index, witness := range view.Witnesses {
		blocks[index] = witness.ReplayBlock
	}
	normalized := protocol.ShastaRequest{
		Schema: req.Schema,
		Payload: protocol.ShastaPayload{
			ChainID:        view.GuestInputChainID,
			Blocks:         blocks,
			ProofCarryData: view.Raw.ProofCarryData,
			GuestInput:     req.Payload.GuestInput,
		},
	}

	validated := &ValidatedRequest{
		Request:     normalized,
		Carry:       view.Carry,
		Blocks:      append([]BlockView(nil), view.Blocks...),
		LogMetadata: metadata,
	}
	sealValidatedRequest(validated)
	return validated, nil
}

func sealValidatedRequest(req *ValidatedRequest) {
	req.validated = true
	req.validatedCarry = req.Carry
	req.validatedBlocks = append([]BlockView(nil), req.Blocks...)
	req.validatedRawBlocks = replayBlockBindings(req.Request.Payload.Blocks)
	req.validatedGuestInput = req.Request.Payload.GuestInput != nil
	if req.validatedGuestInput {
		req.validatedTaiko = string(req.Request.Payload.GuestInput.Taiko)
	}
}

func validateRequestSigningBinding(req *ValidatedRequest) error {
	if req == nil || !req.validated {
		return fmt.Errorf("request validation binding is missing")
	}
	if req.Carry != req.validatedCarry {
		return fmt.Errorf("request validation binding mismatch: proof carry changed after validation")
	}
	if len(req.Blocks) != len(req.validatedBlocks) {
		return fmt.Errorf("request validation binding mismatch: replay block count changed after validation")
	}
	for index := range req.Blocks {
		if req.Blocks[index] != req.validatedBlocks[index] {
			return fmt.Errorf("request validation binding mismatch: replay block %d changed after validation", index)
		}
	}
	rawBlocks := replayBlockBindings(req.Request.Payload.Blocks)
	if len(rawBlocks) != len(req.validatedRawBlocks) {
		return fmt.Errorf("request validation binding mismatch: raw replay block count changed after validation")
	}
	for index := range rawBlocks {
		if rawBlocks[index] != req.validatedRawBlocks[index] {
			return fmt.Errorf("request validation binding mismatch: raw replay block %d changed after validation", index)
		}
	}
	hasGuestInput := req.Request.Payload.GuestInput != nil
	if hasGuestInput != req.validatedGuestInput {
		return fmt.Errorf("request validation binding mismatch: guest input presence changed after validation")
	}
	if hasGuestInput && string(req.Request.Payload.GuestInput.Taiko) != req.validatedTaiko {
		return fmt.Errorf("request validation binding mismatch: taiko guest input changed after validation")
	}
	return nil
}

func replayBlockBindings(blocks []protocol.ReplayBlock) []replayBlockBinding {
	bindings := make([]replayBlockBinding, len(blocks))
	for index, block := range blocks {
		bindings[index] = replayBlockBinding{
			Block:     string(block.Block),
			ChainSpec: string(block.ChainSpec),
			Witness:   string(block.Witness),
			Accounts:  string(block.Accounts),
		}
	}
	return bindings
}

func requestLogMetadata(req protocol.ShastaRequest) RequestLogMetadata {
	return RequestLogMetadata{
		Schema:     req.Schema,
		ChainID:    req.Payload.ChainID,
		BlockCount: len(req.Payload.Blocks),
	}
}

func requestLogMetadataFromView(req protocol.ShastaRequest, view *GuestInputView) RequestLogMetadata {
	metadata := requestLogMetadata(req)
	if view.GuestInputChainID != 0 {
		metadata.ChainID = view.GuestInputChainID
	}
	metadata.BlockCount = len(view.Witnesses)
	return metadata
}

func validationError(err error, metadata RequestLogMetadata) error {
	if err == nil {
		return nil
	}
	return &ValidationError{Err: err, Metadata: metadata}
}

func validateBlockViews(blocks []BlockView, carry CarryView) error {
	for index := 1; index < len(blocks); index++ {
		prev := blocks[index-1]
		current := blocks[index]
		if current.Number != prev.Number+1 {
			return fmt.Errorf(
				"block numbers must be contiguous: got %d after %d",
				current.Number,
				prev.Number,
			)
		}
		if current.ParentHash != prev.Hash {
			return fmt.Errorf(
				"block parent hash mismatch at index %d: got %s expected %s",
				index,
				current.ParentHash.Hex(),
				prev.Hash.Hex(),
			)
		}
	}

	if first := blocks[0]; first.ParentHash != carry.TransitionInput.ParentBlockHash {
		return fmt.Errorf(
			"first block parent hash mismatch: block=%s checkpoint=%s",
			first.ParentHash.Hex(),
			carry.TransitionInput.ParentBlockHash.Hex(),
		)
	}

	last := blocks[len(blocks)-1]
	if last.Number != carry.TransitionInput.Checkpoint.BlockNumber {
		return fmt.Errorf(
			"checkpoint block number mismatch: block=%d checkpoint=%d",
			last.Number,
			carry.TransitionInput.Checkpoint.BlockNumber,
		)
	}
	if last.Hash != carry.TransitionInput.Checkpoint.BlockHash {
		return fmt.Errorf(
			"checkpoint block hash mismatch: block=%s checkpoint=%s",
			last.Hash.Hex(),
			carry.TransitionInput.Checkpoint.BlockHash.Hex(),
		)
	}
	if last.StateRoot != carry.TransitionInput.Checkpoint.StateRoot {
		return fmt.Errorf(
			"checkpoint state root mismatch: block=%s checkpoint=%s",
			last.StateRoot.Hex(),
			carry.TransitionInput.Checkpoint.StateRoot.Hex(),
		)
	}
	return nil
}

func decodeBlock(block protocol.ReplayBlock) (BlockView, error) {
	decoded, err := decodeBlockEnvelope(block.Block)
	if err != nil {
		return BlockView{}, err
	}
	header, err := decodeHeader(decoded.Header)
	if err != nil {
		return BlockView{}, err
	}

	return BlockView{
		Number:       header.Number.Uint64(),
		Hash:         header.Hash(),
		ParentHash:   header.ParentHash,
		StateRoot:    header.Root,
		ReceiptsRoot: header.ReceiptHash,
	}, nil
}

func decodeCarry(raw json.RawMessage) (CarryView, error) {
	return decodeCarryStrict(raw)
}
