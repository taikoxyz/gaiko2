package prover

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	maxUint24 = uint64(1<<24 - 1)
	maxUint48 = uint64(1<<48 - 1)
)

var taikoVerifierForks = []string{"PACAYA", "SHASTA", "UNZEN"}

type shastaProposalView struct {
	ID                             uint64
	Timestamp                      uint64
	EndOfSubmissionWindowTimestamp uint64
	Proposer                       common.Address
	ParentProposalHash             common.Hash
	OriginBlockNumber              uint64
	OriginBlockHash                common.Hash
	BasefeeSharingPctg             uint8
	Sources                        []shastaDerivationSourceView
}

type shastaDerivationSourceView struct {
	IsForcedInclusion bool
	BlobSlice         shastaBlobSliceView
}

type shastaBlobSliceView struct {
	BlobHashes []common.Hash
	Offset     uint64
	Timestamp  uint64
}

type abiShastaProposal struct {
	ID                             *big.Int              `abi:"id"`
	Timestamp                      *big.Int              `abi:"timestamp"`
	EndOfSubmissionWindowTimestamp *big.Int              `abi:"endOfSubmissionWindowTimestamp"`
	Proposer                       common.Address        `abi:"proposer"`
	ParentProposalHash             [32]byte              `abi:"parentProposalHash"`
	OriginBlockNumber              *big.Int              `abi:"originBlockNumber"`
	OriginBlockHash                [32]byte              `abi:"originBlockHash"`
	BasefeeSharingPctg             uint8                 `abi:"basefeeSharingPctg"`
	Sources                        []abiDerivationSource `abi:"sources"`
}

type abiDerivationSource struct {
	IsForcedInclusion bool         `abi:"isForcedInclusion"`
	BlobSlice         abiBlobSlice `abi:"blobSlice"`
}

type abiBlobSlice struct {
	BlobHashes [][32]byte `abi:"blobHashes"`
	Offset     *big.Int   `abi:"offset"`
	Timestamp  *big.Int   `abi:"timestamp"`
}

func ValidateGuestInputCarry(view *GuestInputView) error {
	if view == nil {
		return fmt.Errorf("guest input view is nil")
	}
	if view.GuestInputChainID == 0 {
		return fmt.Errorf("guest input chain id is missing")
	}
	if view.Carry.ChainID != view.GuestInputChainID {
		return fmt.Errorf(
			"chain_id mismatch: guest_input=%d proof_carry_data=%d",
			view.GuestInputChainID,
			view.Carry.ChainID,
		)
	}

	if view.Carry.TransitionInput.ProposalID != view.Taiko.ProposalID {
		return fmt.Errorf(
			"proposal_id mismatch: taiko=%d proof_carry_data=%d",
			view.Taiko.ProposalID,
			view.Carry.TransitionInput.ProposalID,
		)
	}
	if view.Taiko.ProposalID != view.Taiko.ProposalEventProposalID {
		return fmt.Errorf(
			"taiko proposal_id mismatch: taiko.proposal_id=%d taiko.proposal_event.proposal.id=%d",
			view.Taiko.ProposalID,
			view.Taiko.ProposalEventProposalID,
		)
	}

	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		return err
	}
	proposalHash, err := hashShastaProposal(proposal)
	if err != nil {
		return fmt.Errorf("hash taiko.proposal_event.proposal: %w", err)
	}
	if view.Carry.TransitionInput.ProposalHash != proposalHash {
		return fmt.Errorf(
			"proposal_hash mismatch: taiko=%s proof_carry_data=%s",
			proposalHash.Hex(),
			view.Carry.TransitionInput.ProposalHash.Hex(),
		)
	}
	if view.Carry.TransitionInput.ParentProposalHash != proposal.ParentProposalHash {
		return fmt.Errorf(
			"parent_proposal_hash mismatch: taiko=%s proof_carry_data=%s",
			proposal.ParentProposalHash.Hex(),
			view.Carry.TransitionInput.ParentProposalHash.Hex(),
		)
	}

	if len(view.Blocks) == 0 {
		return fmt.Errorf("guest input must include at least one decoded block")
	}
	first := view.Blocks[0]
	if view.Carry.TransitionInput.ParentBlockHash != first.ParentHash {
		return fmt.Errorf(
			"parent_block_hash mismatch: first_block=%s proof_carry_data=%s",
			first.ParentHash.Hex(),
			view.Carry.TransitionInput.ParentBlockHash.Hex(),
		)
	}

	actualProver, err := decodeGuestInputActualProver(view.TaikoRaw)
	if err != nil {
		return err
	}
	if view.Carry.TransitionInput.ActualProver != actualProver {
		return fmt.Errorf(
			"actual_prover mismatch: taiko=%s proof_carry_data=%s",
			actualProver.Hex(),
			view.Carry.TransitionInput.ActualProver.Hex(),
		)
	}
	if view.Carry.TransitionInput.Transition.Proposer != proposal.Proposer {
		return fmt.Errorf(
			"transition.proposer mismatch: proposal=%s proof_carry_data=%s",
			proposal.Proposer.Hex(),
			view.Carry.TransitionInput.Transition.Proposer.Hex(),
		)
	}
	if view.Carry.TransitionInput.Transition.Timestamp != proposal.Timestamp {
		return fmt.Errorf(
			"transition.timestamp mismatch: proposal=%d proof_carry_data=%d",
			proposal.Timestamp,
			view.Carry.TransitionInput.Transition.Timestamp,
		)
	}

	last := view.Blocks[len(view.Blocks)-1]
	checkpoint := view.Carry.TransitionInput.Checkpoint
	if checkpoint.BlockNumber != last.Number {
		return fmt.Errorf(
			"checkpoint.blockNumber mismatch: last_block=%d proof_carry_data=%d",
			last.Number,
			checkpoint.BlockNumber,
		)
	}
	if checkpoint.BlockHash != last.Hash {
		return fmt.Errorf(
			"checkpoint.blockHash mismatch: last_block=%s proof_carry_data=%s",
			last.Hash.Hex(),
			checkpoint.BlockHash.Hex(),
		)
	}
	if checkpoint.StateRoot != last.StateRoot {
		return fmt.Errorf(
			"checkpoint.stateRoot mismatch: last_block=%s proof_carry_data=%s",
			last.StateRoot.Hex(),
			checkpoint.StateRoot.Hex(),
		)
	}

	verifier, err := resolveGuestInputVerifier(view)
	if err != nil {
		return err
	}
	if view.Carry.Verifier != verifier {
		return fmt.Errorf(
			"verifier mismatch: chain_spec=%s proof_carry_data=%s",
			verifier.Hex(),
			view.Carry.Verifier.Hex(),
		)
	}

	return nil
}

func decodeCarryStrict(raw json.RawMessage) (CarryView, error) {
	if isEmptyOrNullRawMessage(raw) {
		return CarryView{}, fmt.Errorf("missing or null proof_carry_data")
	}
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return CarryView{}, fmt.Errorf("unmarshal proof_carry_data: %w", err)
	}

	chainID, err := requireUint64(fields, "chain_id")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.chain_id: %w", err)
	}
	verifier, err := requireAddress(fields, "verifier")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.verifier: %w", err)
	}
	transitionFields, err := requireJSONObjectField(fields, "transition_input")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input: %w", err)
	}

	proposalID, err := requireUint64(transitionFields, "proposal_id")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.proposal_id: %w", err)
	}
	proposalHash, err := requireHash(transitionFields, "proposal_hash")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.proposal_hash: %w", err)
	}
	parentProposalHash, err := requireHash(transitionFields, "parent_proposal_hash")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.parent_proposal_hash: %w", err)
	}
	parentBlockHash, err := requireHash(transitionFields, "parent_block_hash")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.parent_block_hash: %w", err)
	}
	actualProver, err := requireAddress(transitionFields, "actual_prover")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.actual_prover: %w", err)
	}

	transition, err := requireJSONObjectField(transitionFields, "transition")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.transition: %w", err)
	}
	proposer, err := requireAddress(transition, "proposer")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.transition.proposer: %w", err)
	}
	timestamp, err := requireUint64(transition, "timestamp")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.transition.timestamp: %w", err)
	}

	checkpoint, err := requireJSONObjectField(transitionFields, "checkpoint")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint: %w", err)
	}
	blockNumber, err := requireUint64(checkpoint, "blockNumber")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint.blockNumber: %w", err)
	}
	blockHash, err := requireHash(checkpoint, "blockHash")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint.blockHash: %w", err)
	}
	stateRoot, err := requireHash(checkpoint, "stateRoot")
	if err != nil {
		return CarryView{}, fmt.Errorf("parse proof_carry_data.transition_input.checkpoint.stateRoot: %w", err)
	}

	return CarryView{
		ChainID:  chainID,
		Verifier: verifier,
		TransitionInput: TransitionInputView{
			ProposalID:         proposalID,
			ProposalHash:       proposalHash,
			ParentProposalHash: parentProposalHash,
			ParentBlockHash:    parentBlockHash,
			ActualProver:       actualProver,
			Transition: TransitionView{
				Proposer:  proposer,
				Timestamp: timestamp,
			},
			Checkpoint: CheckpointView{
				BlockNumber: blockNumber,
				BlockHash:   blockHash,
				StateRoot:   stateRoot,
			},
		},
	}, nil
}

func decodeGuestInputTaikoProposal(raw json.RawMessage) (shastaProposalView, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return shastaProposalView{}, fmt.Errorf("unmarshal taiko: %w", err)
	}
	eventRaw, ok := lookupField(fields, "proposal_event", "proposalEvent")
	if !ok {
		return shastaProposalView{}, fmt.Errorf("missing taiko.proposal_event")
	}
	eventFields, err := decodeJSONObject(eventRaw)
	if err != nil {
		return shastaProposalView{}, fmt.Errorf("unmarshal taiko.proposal_event: %w", err)
	}
	proposalRaw, ok := lookupField(eventFields, "proposal")
	if !ok {
		return shastaProposalView{}, fmt.Errorf("missing taiko.proposal_event.proposal")
	}
	proposal, err := decodeShastaProposal(proposalRaw)
	if err != nil {
		return shastaProposalView{}, fmt.Errorf("decode taiko.proposal_event.proposal: %w", err)
	}
	return proposal, nil
}

func decodeGuestInputActualProver(raw json.RawMessage) (common.Address, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal taiko: %w", err)
	}
	proverDataRaw, ok := lookupField(fields, "prover_data", "proverData")
	if !ok {
		return common.Address{}, fmt.Errorf("missing taiko.prover_data")
	}
	proverData, err := decodeJSONObject(proverDataRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal taiko.prover_data: %w", err)
	}
	rawActualProver, ok := lookupField(proverData, "actual_prover", "actualProver")
	if !ok {
		return common.Address{}, fmt.Errorf("missing taiko.prover_data.actual_prover")
	}
	actualProver, err := parseAddressJSON(rawActualProver)
	if err != nil {
		return common.Address{}, fmt.Errorf("parse taiko.prover_data.actual_prover: %w", err)
	}
	return actualProver, nil
}

func resolveGuestInputVerifier(view *GuestInputView) (common.Address, error) {
	if len(view.Witnesses) == 0 {
		return common.Address{}, fmt.Errorf("guest input must include at least one witness")
	}
	firstHeader, err := decodeFirstWitnessHeader(view)
	if err != nil {
		return common.Address{}, err
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	activeForkIndex, forkActive, err := activeTaikoFork(fields, firstHeader.Number.Uint64(), firstHeader.Time)
	if err != nil {
		return common.Address{}, err
	}
	rawForks, ok := lookupField(fields, "verifier_address_forks", "verifierAddressForks")
	if !ok {
		return common.Address{}, fmt.Errorf("missing verifier_address_forks in witness.chain_spec")
	}
	forks, err := decodeJSONObject(rawForks)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec.verifier_address_forks: %w", err)
	}

	for index := activeForkIndex; index >= 0; index-- {
		forkName := taikoVerifierForks[index]
		if !forkActive[forkName] {
			continue
		}
		rawFork, ok := lookupField(forks, forkName)
		if !ok {
			continue
		}
		fork, err := decodeJSONObject(rawFork)
		if err != nil {
			return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec.verifier_address_forks.%s: %w", forkName, err)
		}
		rawVerifier, ok := lookupField(fork, "Sgx", "SGX", "sgx", "SgxGeth", "SGXGETH", "sgxgeth", "sgx_geth")
		if !ok {
			continue
		}
		verifier, err := parseAddressJSON(rawVerifier)
		if err != nil {
			return common.Address{}, fmt.Errorf("parse witness.chain_spec.verifier_address_forks.%s.Sgx: %w", forkName, err)
		}
		return verifier, nil
	}

	return common.Address{}, fmt.Errorf("missing verifier for active Sgx in witness.chain_spec.verifier_address_forks")
}

func decodeFirstWitnessHeader(view *GuestInputView) (*typesHeaderView, error) {
	decoded, err := decodeBlockEnvelope(view.Witnesses[0].BlockRaw)
	if err != nil {
		return nil, fmt.Errorf("decode first witness block: %w", err)
	}
	header, err := decodeHeader(decoded.Header)
	if err != nil {
		return nil, fmt.Errorf("decode first witness header: %w", err)
	}
	return &typesHeaderView{
		Number: header.Number,
		Time:   header.Time,
	}, nil
}

type typesHeaderView struct {
	Number *big.Int
	Time   uint64
}

func activeTaikoFork(
	chainSpec map[string]json.RawMessage,
	blockNumber uint64,
	blockTimestamp uint64,
) (int, map[string]bool, error) {
	rawHardForks, ok := lookupField(chainSpec, "hard_forks", "hardForks")
	if !ok {
		return 0, nil, fmt.Errorf("missing hard_forks in witness.chain_spec")
	}
	hardForks, err := decodeJSONObject(rawHardForks)
	if err != nil {
		return 0, nil, fmt.Errorf("unmarshal witness.chain_spec.hard_forks: %w", err)
	}

	activeIndex := -1
	active := make(map[string]bool, len(taikoVerifierForks))
	for index, forkName := range taikoVerifierForks {
		rawFork, ok := lookupForkCaseInsensitive(hardForks, forkName)
		if !ok {
			continue
		}
		isActive, err := hardForkActive(rawFork, blockNumber, blockTimestamp)
		if err != nil {
			return 0, nil, fmt.Errorf("parse witness.chain_spec.hard_forks.%s: %w", forkName, err)
		}
		active[forkName] = isActive
		if isActive {
			activeIndex = index
		}
	}
	if activeIndex < 0 {
		return 0, nil, fmt.Errorf("no active Taiko verifier fork for first witness block")
	}
	return activeIndex, active, nil
}

func hardForkActive(raw json.RawMessage, blockNumber uint64, blockTimestamp uint64) (bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.EqualFold(text, "Tbd") {
			return false, nil
		}
		return false, fmt.Errorf("unknown hard fork string %q", text)
	}

	fields, err := decodeJSONObject(raw)
	if err != nil {
		return false, err
	}
	block, err := optionalUint64Ptr(fields, "Block", "block")
	if err != nil {
		return false, err
	}
	if block != nil {
		return blockNumber >= *block, nil
	}
	timestamp, err := optionalUint64Ptr(fields, "Timestamp", "timestamp")
	if err != nil {
		return false, err
	}
	if timestamp != nil {
		return blockTimestamp >= *timestamp, nil
	}
	return false, nil
}

func lookupForkCaseInsensitive(fields map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if raw, ok := fields[name]; ok {
		return raw, true
	}
	for key, raw := range fields {
		if strings.EqualFold(key, name) {
			return raw, true
		}
	}
	return nil, false
}

func decodeShastaProposal(raw json.RawMessage) (shastaProposalView, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return shastaProposalView{}, fmt.Errorf("unmarshal proposal: %w", err)
	}

	id, err := requireUint48(fields, "id")
	if err != nil {
		return shastaProposalView{}, err
	}
	timestamp, err := requireUint48(fields, "timestamp")
	if err != nil {
		return shastaProposalView{}, err
	}
	endOfSubmissionWindowTimestamp, err := requireUint48(fields, "endOfSubmissionWindowTimestamp", "end_of_submission_window_timestamp")
	if err != nil {
		return shastaProposalView{}, err
	}
	proposer, err := requireAddress(fields, "proposer")
	if err != nil {
		return shastaProposalView{}, err
	}
	parentProposalHash, err := requireHash(fields, "parentProposalHash", "parent_proposal_hash")
	if err != nil {
		return shastaProposalView{}, err
	}
	originBlockNumber, err := requireUint48(fields, "originBlockNumber", "origin_block_number")
	if err != nil {
		return shastaProposalView{}, err
	}
	originBlockHash, err := requireHash(fields, "originBlockHash", "origin_block_hash")
	if err != nil {
		return shastaProposalView{}, err
	}
	basefeeSharingPctg, err := requireUint8(fields, "basefeeSharingPctg", "basefee_sharing_pctg")
	if err != nil {
		return shastaProposalView{}, err
	}
	sources, err := requireDerivationSources(fields, "sources")
	if err != nil {
		return shastaProposalView{}, err
	}

	return shastaProposalView{
		ID:                             id,
		Timestamp:                      timestamp,
		EndOfSubmissionWindowTimestamp: endOfSubmissionWindowTimestamp,
		Proposer:                       proposer,
		ParentProposalHash:             parentProposalHash,
		OriginBlockNumber:              originBlockNumber,
		OriginBlockHash:                originBlockHash,
		BasefeeSharingPctg:             basefeeSharingPctg,
		Sources:                        sources,
	}, nil
}

func hashShastaProposal(proposal shastaProposalView) (common.Hash, error) {
	proposalType, err := shastaProposalABIType()
	if err != nil {
		return common.Hash{}, err
	}
	encoded, err := abi.Arguments{{Type: proposalType}}.Pack(proposal.toABI())
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(encoded), nil
}

func shastaProposalABIType() (abi.Type, error) {
	return abi.NewType("tuple", "struct Shasta.Proposal", []abi.ArgumentMarshaling{
		{Name: "id", Type: "uint48"},
		{Name: "timestamp", Type: "uint48"},
		{Name: "endOfSubmissionWindowTimestamp", Type: "uint48"},
		{Name: "proposer", Type: "address"},
		{Name: "parentProposalHash", Type: "bytes32"},
		{Name: "originBlockNumber", Type: "uint48"},
		{Name: "originBlockHash", Type: "bytes32"},
		{Name: "basefeeSharingPctg", Type: "uint8"},
		{Name: "sources", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "isForcedInclusion", Type: "bool"},
			{Name: "blobSlice", Type: "tuple", Components: []abi.ArgumentMarshaling{
				{Name: "blobHashes", Type: "bytes32[]"},
				{Name: "offset", Type: "uint24"},
				{Name: "timestamp", Type: "uint48"},
			}},
		}},
	})
}

func (proposal shastaProposalView) toABI() abiShastaProposal {
	sources := make([]abiDerivationSource, len(proposal.Sources))
	for i, source := range proposal.Sources {
		blobHashes := make([][32]byte, len(source.BlobSlice.BlobHashes))
		for j, hash := range source.BlobSlice.BlobHashes {
			blobHashes[j] = [32]byte(hash)
		}
		sources[i] = abiDerivationSource{
			IsForcedInclusion: source.IsForcedInclusion,
			BlobSlice: abiBlobSlice{
				BlobHashes: blobHashes,
				Offset:     uint64Big(source.BlobSlice.Offset),
				Timestamp:  uint64Big(source.BlobSlice.Timestamp),
			},
		}
	}

	return abiShastaProposal{
		ID:                             uint64Big(proposal.ID),
		Timestamp:                      uint64Big(proposal.Timestamp),
		EndOfSubmissionWindowTimestamp: uint64Big(proposal.EndOfSubmissionWindowTimestamp),
		Proposer:                       proposal.Proposer,
		ParentProposalHash:             [32]byte(proposal.ParentProposalHash),
		OriginBlockNumber:              uint64Big(proposal.OriginBlockNumber),
		OriginBlockHash:                [32]byte(proposal.OriginBlockHash),
		BasefeeSharingPctg:             proposal.BasefeeSharingPctg,
		Sources:                        sources,
	}
}

func requireUint48(fields map[string]json.RawMessage, names ...string) (uint64, error) {
	value, err := requireUint64(fields, names...)
	if err != nil {
		return 0, err
	}
	if value > maxUint48 {
		return 0, fmt.Errorf("field %q exceeds uint48", names[0])
	}
	return value, nil
}

func requireUint24(fields map[string]json.RawMessage, names ...string) (uint64, error) {
	value, err := requireUint64(fields, names...)
	if err != nil {
		return 0, err
	}
	if value > maxUint24 {
		return 0, fmt.Errorf("field %q exceeds uint24", names[0])
	}
	return value, nil
}

func requireUint8(fields map[string]json.RawMessage, names ...string) (uint8, error) {
	value, err := requireUint64(fields, names...)
	if err != nil {
		return 0, err
	}
	if value > 0xff {
		return 0, fmt.Errorf("field %q exceeds uint8", names[0])
	}
	return uint8(value), nil
}

func requireBool(fields map[string]json.RawMessage, names ...string) (bool, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return false, fmt.Errorf("missing required field %q", names[0])
	}
	if isEmptyOrNullRawMessage(raw) {
		return false, fmt.Errorf("missing or null required field %q", names[0])
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func requireDerivationSources(fields map[string]json.RawMessage, names ...string) ([]shastaDerivationSourceView, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", names[0])
	}
	if isEmptyOrNullRawMessage(raw) {
		return nil, fmt.Errorf("field %q must be an array", names[0])
	}
	var rawSources []json.RawMessage
	if err := json.Unmarshal(raw, &rawSources); err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}

	sources := make([]shastaDerivationSourceView, len(rawSources))
	for i, rawSource := range rawSources {
		source, err := decodeDerivationSource(rawSource)
		if err != nil {
			return nil, fmt.Errorf("parse field %q[%d]: %w", names[0], i, err)
		}
		sources[i] = source
	}
	return sources, nil
}

func decodeDerivationSource(raw json.RawMessage) (shastaDerivationSourceView, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return shastaDerivationSourceView{}, err
	}
	isForcedInclusion, err := requireBool(fields, "isForcedInclusion", "is_forced_inclusion")
	if err != nil {
		return shastaDerivationSourceView{}, err
	}
	rawBlobSlice, ok := lookupField(fields, "blobSlice", "blob_slice")
	if !ok {
		return shastaDerivationSourceView{}, fmt.Errorf("missing required field %q", "blobSlice")
	}
	blobSlice, err := decodeBlobSlice(rawBlobSlice)
	if err != nil {
		return shastaDerivationSourceView{}, fmt.Errorf("parse field %q: %w", "blobSlice", err)
	}
	return shastaDerivationSourceView{
		IsForcedInclusion: isForcedInclusion,
		BlobSlice:         blobSlice,
	}, nil
}

func decodeBlobSlice(raw json.RawMessage) (shastaBlobSliceView, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return shastaBlobSliceView{}, err
	}
	rawBlobHashes, ok := lookupField(fields, "blobHashes", "blob_hashes")
	if !ok {
		return shastaBlobSliceView{}, fmt.Errorf("missing required field %q", "blobHashes")
	}
	if isEmptyOrNullRawMessage(rawBlobHashes) {
		return shastaBlobSliceView{}, fmt.Errorf("field %q must be an array", "blobHashes")
	}
	var rawHashes []json.RawMessage
	if err := json.Unmarshal(rawBlobHashes, &rawHashes); err != nil {
		return shastaBlobSliceView{}, fmt.Errorf("parse field %q: %w", "blobHashes", err)
	}
	blobHashes := make([]common.Hash, len(rawHashes))
	for i, rawHash := range rawHashes {
		hash, err := parseHashJSON(rawHash)
		if err != nil {
			return shastaBlobSliceView{}, fmt.Errorf("parse field %q[%d]: %w", "blobHashes", i, err)
		}
		blobHashes[i] = hash
	}
	offset, err := requireUint24(fields, "offset")
	if err != nil {
		return shastaBlobSliceView{}, err
	}
	timestamp, err := requireUint48(fields, "timestamp")
	if err != nil {
		return shastaBlobSliceView{}, err
	}
	return shastaBlobSliceView{
		BlobHashes: blobHashes,
		Offset:     offset,
		Timestamp:  timestamp,
	}, nil
}

func uint64Big(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}

func requireJSONObjectField(fields map[string]json.RawMessage, names ...string) (map[string]json.RawMessage, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", names[0])
	}
	if isEmptyOrNullRawMessage(raw) {
		return nil, fmt.Errorf("missing or null field %q", names[0])
	}
	value, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	return value, nil
}
