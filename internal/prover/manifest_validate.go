package prover

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	shastaPayloadVersion             = 1
	shastaAnchorGasLimit             = 1_000_000
	shastaManifestMainnetChain       = 167000
	shastaMaxAnchorOffset            = 128
	shastaMaxAnchorOffsetMainnet     = 512
	shastaMinBlockGasLimit           = 10_000_000
	shastaMaxBlockGasLimit           = 45_000_000
	shastaBlockGasLimitMaxChange     = 200
	shastaGasLimitDenominator        = 1_000_000
	shastaDerivationSourceMaxBlocks  = 192
	shastaUnzenDerivationSourceLimit = 768
	shastaMaxManifestOffset          = shastaBytesPerBlob - 64
	shastaMaxManifestDecodedPayload  = 16 * 1024 * 1024
	shastaMaxManifestTxsPerBlock     = shastaMaxBlockGasLimit / params.TxGas
	shastaTaikoL2AddressSuffix       = "10001"
	shastaCheckpointStoreSuffix      = "5"
	shastaGoldenTouchPrivateKey      = nativeProofPrivateKey
)

const shastaSignalServiceCheckpointsSlot uint64 = 254

const anchorV4CalldataLength = 4 + 96

var shastaGoldenTouchAccount = common.HexToAddress("0x0000777735367b36bC9B61C50022d9D0700dB4Ec")

type shastaSourceManifest struct {
	Blocks []shastaManifestBlock
}

type shastaManifestBlock struct {
	Timestamp         uint64
	Coinbase          common.Address
	AnchorBlockNumber uint64
	GasLimit          uint64
	Transactions      types.Transactions
}

type shastaManifestParentContext struct {
	Header            *types.Header
	GrandparentHeader *CompactAncestor
	AnchorBlockNumber uint64
}

type shastaProposalAncestorHeader struct {
	Full    *types.Header
	Compact CompactAncestor
}

type manifestAnchorSourceSpan struct {
	isForcedInclusion bool
	blockCount        int
}

func ValidateGuestInputManifestBinding(view *GuestInputView) error {
	return ValidateGuestInputManifestBindingWithContext(context.Background(), view)
}

func ValidateGuestInputManifestBindingWithContext(ctx context.Context, view *GuestInputView) error {
	if view == nil {
		return fmt.Errorf("guest input view is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		return err
	}
	if len(proposal.Sources) == 0 {
		return fmt.Errorf("proposal sources must not be empty")
	}
	if len(view.DataSourcesRaw) != len(proposal.Sources) {
		return fmt.Errorf(
			"data source count mismatch: proposal_sources=%d data_sources=%d",
			len(proposal.Sources),
			len(view.DataSourcesRaw),
		)
	}

	parentHeader, grandparentHeader, err := decodeProposalAncestorHeaderContext(view.Raw.ProposalAncestorHeaders)
	if err != nil {
		return err
	}
	if parentHeader.Hash() != view.Carry.TransitionInput.ParentBlockHash {
		return fmt.Errorf(
			"proposal parent header hash mismatch: got %s expected %s",
			parentHeader.Hash().Hex(),
			view.Carry.TransitionInput.ParentBlockHash.Hex(),
		)
	}
	hostAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
	if err != nil {
		return err
	}
	if hostAnchor == nil {
		return fmt.Errorf("missing taiko.prover_data.last_anchor_block_number")
	}
	lastAnchor := *hostAnchor
	parent := shastaManifestParentContext{
		Header:            parentHeader,
		GrandparentHeader: grandparentHeader,
		AnchorBlockNumber: lastAnchor,
	}
	forkTimestamp, maxBlocks, err := shastaDerivationConfig(view.GuestInputChainID, proposal.Timestamp)
	if err != nil {
		return err
	}

	derived := make([]shastaManifestBlock, 0, len(view.Witnesses))
	sourceSpans := make([]manifestAnchorSourceSpan, 0, len(proposal.Sources))
	for sourceIndex, source := range proposal.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		dataSource, err := decodeBlobSourceData(view.DataSourcesRaw[sourceIndex])
		if err != nil {
			return fmt.Errorf("decode data_sources[%d]: %w", sourceIndex, err)
		}
		manifest, err := prepareSourceManifest(
			dataSource,
			source,
			parent,
			proposal,
			view.GuestInputChainID,
			forkTimestamp,
			maxBlocks,
		)
		if err != nil {
			return fmt.Errorf("prepare source manifest %d: %w", sourceIndex, err)
		}

		sourceSpans = append(sourceSpans, manifestAnchorSourceSpan{
			isForcedInclusion: source.IsForcedInclusion,
			blockCount:        len(manifest.Blocks),
		})
		for _, block := range manifest.Blocks {
			derived = append(derived, block)
			parent = shastaManifestParentContext{
				Header: &types.Header{
					Number:    new(big.Int).SetUint64(parent.Header.Number.Uint64() + 1),
					Time:      block.Timestamp,
					GasLimit:  block.GasLimit + shastaAnchorGasLimit,
					MixDigest: shastaManifestMixHash(parent.Header.MixDigest, parent.Header.Number.Uint64()+1),
				},
				AnchorBlockNumber: block.AnchorBlockNumber,
			}
		}
	}

	if len(derived) != len(view.Witnesses) {
		return fmt.Errorf(
			"derived manifest block count mismatch: derived=%d witnesses=%d",
			len(derived),
			len(view.Witnesses),
		)
	}
	anchorBlockNumbers := make([]uint64, 0, len(derived))
	for _, block := range derived {
		anchorBlockNumbers = append(anchorBlockNumbers, block.AnchorBlockNumber)
	}
	if err := validateSourceAwareManifestAnchors(
		anchorBlockNumbers,
		sourceSpans,
		lastAnchor,
		proposal.OriginBlockNumber,
		view.GuestInputChainID,
	); err != nil {
		return err
	}

	canonicalParent := parentHeader
	canonicalGrandparent := grandparentHeader
	checkpoints := make([]anchorV4CheckpointView, 0, len(derived))
	for index, expectedBlock := range derived {
		if err := ctx.Err(); err != nil {
			return err
		}
		block, witness, err := decodeReplayBlock(view.Witnesses[index].ReplayBlock)
		if err != nil {
			return fmt.Errorf("decode witness block %d: %w", index, err)
		}
		checkpoint, err := validateManifestBlockBinding(ctx, view, proposal, block, witness, expectedBlock, canonicalParent, canonicalGrandparent)
		if err != nil {
			return fmt.Errorf("manifest block %d: %w", index, err)
		}
		checkpoints = append(checkpoints, checkpoint)
		rolledGrandparent := compactAncestorFromHeader(canonicalParent)
		canonicalGrandparent = &rolledGrandparent
		canonicalParent = block.Header()
	}

	if err := validateAnchorL1Linkage(view, proposal, checkpoints, sourceSpans, lastAnchor); err != nil {
		return err
	}
	return nil
}

func decodeProposalAncestorHeaderContext(raws []json.RawMessage) (*types.Header, *CompactAncestor, error) {
	headers := make([]shastaProposalAncestorHeader, 0, len(raws))
	for index, raw := range raws {
		var decoded rawWitnessHeader
		if err := json.Unmarshal(raw, &decoded); err == nil && !isEmptyOrNullRawMessage(decoded.Header) {
			header, err := decodeHeader(decoded.Header)
			if err != nil {
				return nil, nil, fmt.Errorf("decode proposal ancestor header %d: %w", index, err)
			}
			headers = append(headers, shastaProposalAncestorHeader{
				Full:    header,
				Compact: compactAncestorFromHeader(header),
			})
			continue
		}

		compact, err := decodeCompactAncestorHeader(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decode proposal ancestor header %d: %w", index, err)
		}
		headers = append(headers, shastaProposalAncestorHeader{Compact: compact})
	}
	if len(headers) == 0 {
		return nil, nil, fmt.Errorf("missing proposal ancestor header")
	}
	parent := headers[len(headers)-1].Full
	if parent == nil {
		return nil, nil, fmt.Errorf("missing full parent header in proposal ancestor headers")
	}
	if len(headers) == 1 {
		return parent, nil, nil
	}
	grandparentHeader := headers[len(headers)-2].Full
	if grandparentHeader == nil {
		return nil, nil, fmt.Errorf("missing full grandparent header in proposal ancestor headers")
	}
	grandparent := compactAncestorFromHeader(grandparentHeader)
	return parent, &grandparent, nil
}

func compactAncestorFromHeader(header *types.Header) CompactAncestor {
	return CompactAncestor{
		Number:     header.Number.Uint64(),
		Hash:       header.Hash(),
		ParentHash: header.ParentHash,
		Timestamp:  header.Time,
	}
}

func decodeCompactAncestorHeader(raw json.RawMessage) (CompactAncestor, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return CompactAncestor{}, err
	}
	number, err := requireUint64(fields, "number")
	if err != nil {
		return CompactAncestor{}, err
	}
	hash, err := requireHash(fields, "hash")
	if err != nil {
		return CompactAncestor{}, err
	}
	parentHash, err := requireHash(fields, "parent_hash", "parentHash")
	if err != nil {
		return CompactAncestor{}, err
	}
	timestamp, err := requireUint64(fields, "timestamp")
	if err != nil {
		return CompactAncestor{}, err
	}
	return CompactAncestor{
		Number:     number,
		Hash:       hash,
		ParentHash: parentHash,
		Timestamp:  timestamp,
	}, nil
}

func prepareSourceManifest(
	dataSource blobSourceDataView,
	source shastaDerivationSourceView,
	parent shastaManifestParentContext,
	proposal shastaProposalView,
	chainID uint64,
	forkTimestamp uint64,
	maxBlocks int,
) (shastaSourceManifest, error) {
	var (
		manifest shastaSourceManifest
		err      error
	)
	if len(source.BlobSlice.BlobHashes) == 0 || source.BlobSlice.Offset > shastaMaxManifestOffset {
		manifest = defaultSourceManifest()
	} else {
		manifest, err = decodeBlobBackedSourceManifest(dataSource, source.BlobSlice.Offset, maxBlocks)
		if err != nil {
			return shastaSourceManifest{}, err
		}
	}

	if source.IsForcedInclusion && len(manifest.Blocks) != 1 {
		manifest = defaultSourceManifest()
	}
	if source.IsForcedInclusion || isDefaultSourceManifest(manifest) {
		applyInheritedManifestMetadata(&manifest, parent, proposal, chainID, forkTimestamp)
	}

	if !validateSourceManifest(manifest, source, parent, proposal, chainID, forkTimestamp) {
		manifest = defaultSourceManifest()
		applyInheritedManifestMetadata(&manifest, parent, proposal, chainID, forkTimestamp)
	}

	return manifest, nil
}

func decodeBlobBackedSourceManifest(dataSource blobSourceDataView, offset uint64, maxBlocks int) (shastaSourceManifest, error) {
	if len(dataSource.TxDataFromBlob) == 0 {
		return shastaSourceManifest{}, fmt.Errorf("blob-backed derivation source is missing blob data")
	}

	// Blob length is host-input integrity (like raiko2's InvalidBlobLength), so validate every
	// blob up front and hard-error, mirroring raiko2's collect-then-decode ordering. This keeps
	// a wrong-length blob a hard error even when another blob is undecodable proposal content.
	for index, raw := range dataSource.TxDataFromBlob {
		if len(raw) != shastaBytesPerBlob {
			return shastaSourceManifest{}, fmt.Errorf("blob %d has invalid length %d; expected %d", index, len(raw), shastaBytesPerBlob)
		}
	}

	var concatenated []byte
	for _, raw := range dataSource.TxDataFromBlob {
		decoded, err := decodeShastaBlob(raw)
		if err != nil {
			// Undecodable blob content is bound to the on-chain blob hashes, so it is an
			// objective property of the proposal (not host-controlled). It degrades to the
			// default manifest exactly like the driver's resolve_source_manifest
			// (raiko2 #137 / taiko-client-rs #21854) instead of failing derivation.
			return defaultSourceManifest(), nil
		}
		concatenated = append(concatenated, decoded...)
	}

	manifest, err := decodeManifestPayload(concatenated, offset, maxBlocks)
	if err != nil {
		return defaultSourceManifest(), nil
	}
	return manifest, nil
}

func decodeManifestPayload(payload []byte, offset uint64, maxBlocks int) (shastaSourceManifest, error) {
	if offset > uint64(len(payload)) {
		return shastaSourceManifest{}, fmt.Errorf("manifest offset %d exceeds payload length %d", offset, len(payload))
	}
	payload = payload[offset:]
	if len(payload) < 64 {
		return shastaSourceManifest{}, fmt.Errorf("manifest payload too short")
	}
	if !bytes.Equal(payload[:24], make([]byte, 24)) {
		return shastaSourceManifest{}, fmt.Errorf("manifest payload version exceeds uint64")
	}
	if version := binary.BigEndian.Uint64(payload[24:32]); version != shastaPayloadVersion {
		return shastaSourceManifest{}, fmt.Errorf("unsupported manifest payload version %d", version)
	}
	size := binary.BigEndian.Uint64(payload[56:64])
	if size > uint64(len(payload)-64) {
		return shastaSourceManifest{}, fmt.Errorf("manifest compressed payload too short")
	}
	zr, err := zlib.NewReader(bytes.NewReader(payload[64 : 64+size]))
	if err != nil {
		return shastaSourceManifest{}, fmt.Errorf("open manifest zlib stream: %w", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(io.LimitReader(zr, shastaMaxManifestDecodedPayload+1))
	if err != nil {
		return shastaSourceManifest{}, fmt.Errorf("decompress manifest payload: %w", err)
	}
	if len(decoded) > shastaMaxManifestDecodedPayload {
		return shastaSourceManifest{}, fmt.Errorf("decompressed manifest payload exceeds %d bytes", shastaMaxManifestDecodedPayload)
	}

	var manifest shastaSourceManifest
	if err := rlp.DecodeBytes(decoded, &manifest); err != nil {
		return shastaSourceManifest{}, fmt.Errorf("decode manifest rlp: %w", err)
	}
	if len(manifest.Blocks) > maxBlocks {
		return shastaSourceManifest{}, fmt.Errorf("manifest block count %d exceeds max %d", len(manifest.Blocks), maxBlocks)
	}
	for index, block := range manifest.Blocks {
		if len(block.Transactions) > int(shastaMaxManifestTxsPerBlock) {
			return shastaSourceManifest{}, fmt.Errorf(
				"manifest block %d transaction count %d exceeds max %d",
				index,
				len(block.Transactions),
				shastaMaxManifestTxsPerBlock,
			)
		}
	}
	return manifest, nil
}

func defaultSourceManifest() shastaSourceManifest {
	return shastaSourceManifest{Blocks: []shastaManifestBlock{{}}}
}

func isDefaultSourceManifest(manifest shastaSourceManifest) bool {
	if len(manifest.Blocks) != 1 {
		return false
	}
	block := manifest.Blocks[0]
	return block.Timestamp == 0 &&
		block.Coinbase == (common.Address{}) &&
		block.AnchorBlockNumber == 0 &&
		block.GasLimit == 0 &&
		len(block.Transactions) == 0
}

func applyInheritedManifestMetadata(
	manifest *shastaSourceManifest,
	parent shastaManifestParentContext,
	proposal shastaProposalView,
	chainID uint64,
	forkTimestamp uint64,
) {
	parentTime := parent.Header.Time
	parentGasLimit := effectiveManifestParentGasLimit(parent.Header.Number.Uint64(), parent.Header.GasLimit)
	for index := range manifest.Blocks {
		timestamp := manifestTimestampLowerBound(parentTime, proposal.Timestamp, forkTimestamp, chainID)
		manifest.Blocks[index].Timestamp = timestamp
		manifest.Blocks[index].Coinbase = proposal.Proposer
		manifest.Blocks[index].AnchorBlockNumber = parent.AnchorBlockNumber
		manifest.Blocks[index].GasLimit = parentGasLimit
		parentTime = timestamp
	}
}

func manifestTimestampLowerBound(
	parentTimestamp uint64,
	proposalTimestamp uint64,
	forkTimestamp uint64,
	chainID uint64,
) uint64 {
	offset := uint64(12 * 128)
	if chainID == shastaManifestMainnetChain {
		offset = 12 * 512
	}
	lower := parentTimestamp + 1
	if proposalTimestamp > offset && lower < proposalTimestamp-offset {
		lower = proposalTimestamp - offset
	}
	if lower < forkTimestamp {
		lower = forkTimestamp
	}
	return lower
}

func effectiveManifestParentGasLimit(parentBlockNumber uint64, parentGasLimit uint64) uint64 {
	if parentBlockNumber == 0 {
		return parentGasLimit
	}
	if parentGasLimit < shastaAnchorGasLimit {
		return 0
	}
	return parentGasLimit - shastaAnchorGasLimit
}

func validateSourceManifest(
	manifest shastaSourceManifest,
	source shastaDerivationSourceView,
	parent shastaManifestParentContext,
	proposal shastaProposalView,
	chainID uint64,
	forkTimestamp uint64,
) bool {
	if len(manifest.Blocks) == 0 {
		return false
	}
	return validateManifestTimestamps(manifest, parent.Header.Time, proposal.Timestamp, forkTimestamp, chainID) &&
		validateManifestAnchorNumbers(manifest, proposal.OriginBlockNumber, parent.AnchorBlockNumber, source.IsForcedInclusion, chainID) &&
		validateManifestGasLimit(manifest, parent.Header.Number.Uint64(), parent.Header.GasLimit)
}

func validateManifestTimestamps(
	manifest shastaSourceManifest,
	parentTimestamp uint64,
	proposalTimestamp uint64,
	forkTimestamp uint64,
	chainID uint64,
) bool {
	parentTime := parentTimestamp
	for _, block := range manifest.Blocks {
		lower := manifestTimestampLowerBound(parentTime, proposalTimestamp, forkTimestamp, chainID)
		if lower > proposalTimestamp {
			return false
		}
		if block.Timestamp < lower || block.Timestamp > proposalTimestamp {
			return false
		}
		parentTime = block.Timestamp
	}
	return true
}

func validateManifestAnchorNumbers(
	manifest shastaSourceManifest,
	originBlockNumber uint64,
	parentAnchorBlockNumber uint64,
	isForcedInclusion bool,
	chainID uint64,
) bool {
	anchors := make([]uint64, 0, len(manifest.Blocks))
	for _, block := range manifest.Blocks {
		anchors = append(anchors, block.AnchorBlockNumber)
	}

	if isForcedInclusion {
		for _, anchor := range anchors {
			if anchor != parentAnchorBlockNumber {
				return false
			}
		}
		return true
	}

	if err := validateManifestAnchorProgression(
		anchors,
		parentAnchorBlockNumber,
		originBlockNumber,
		chainID,
	); err != nil {
		return false
	}
	highestAnchorBlockNumber := parentAnchorBlockNumber
	for _, anchor := range anchors {
		if anchor > highestAnchorBlockNumber {
			highestAnchorBlockNumber = anchor
		}
	}
	return highestAnchorBlockNumber > parentAnchorBlockNumber
}

func validateSourceAwareManifestAnchors(
	anchorBlockNumbers []uint64,
	sourceSpans []manifestAnchorSourceSpan,
	lastAnchorBlockNumber uint64,
	originBlockNumber uint64,
	chainID uint64,
) error {
	if len(anchorBlockNumbers) == 0 {
		return fmt.Errorf("anchor block numbers must not be empty")
	}
	if len(sourceSpans) == 0 {
		return fmt.Errorf("source spans must not be empty")
	}
	normalSource := sourceSpans[len(sourceSpans)-1]
	if normalSource.isForcedInclusion {
		return fmt.Errorf("last Shasta derivation source must be a normal source")
	}
	if normalSource.blockCount == 0 {
		return fmt.Errorf("normal Shasta derivation source must contain at least one block")
	}

	cursor := 0
	for sourceIndex, span := range sourceSpans[:len(sourceSpans)-1] {
		if !span.isForcedInclusion {
			return fmt.Errorf("Shasta derivation source %d must be forced inclusion; only the final source may be normal", sourceIndex)
		}
		if span.blockCount == 0 {
			return fmt.Errorf("forced inclusion source %d must contain at least one block", sourceIndex)
		}
		end := cursor + span.blockCount
		if end < cursor || end > len(anchorBlockNumbers) {
			return fmt.Errorf("source spans cover more blocks than anchor block numbers at source %d", sourceIndex)
		}
		for _, anchor := range anchorBlockNumbers[cursor:end] {
			if anchor != lastAnchorBlockNumber {
				return fmt.Errorf(
					"forced inclusion source %d anchor %d must equal parent anchor %d",
					sourceIndex,
					anchor,
					lastAnchorBlockNumber,
				)
			}
		}
		cursor = end
	}

	end := cursor + normalSource.blockCount
	if end < cursor {
		return fmt.Errorf("normal Shasta derivation source block count overflow")
	}
	if end != len(anchorBlockNumbers) {
		return fmt.Errorf("source spans cover %d blocks but anchor block numbers have %d", end, len(anchorBlockNumbers))
	}
	// The all-stay out-of-range case is bound later against the parent L2 CheckpointStore.
	if shouldBypassStalledAnchorLinkage(anchorBlockNumbers, lastAnchorBlockNumber, originBlockNumber, chainID) {
		return nil
	}
	return validateManifestAnchorProgression(
		anchorBlockNumbers[cursor:end],
		lastAnchorBlockNumber,
		originBlockNumber,
		chainID,
	)
}

func validateManifestAnchorProgression(
	anchorBlockNumbers []uint64,
	lastAnchorBlockNumber uint64,
	originBlockNumber uint64,
	chainID uint64,
) error {
	if len(anchorBlockNumbers) == 0 {
		return fmt.Errorf("anchor block numbers must not be empty")
	}
	minAnchorBlockNumber := uint64(0)
	if originBlockNumber > anchorMaxOffsetForChain(chainID) {
		minAnchorBlockNumber = originBlockNumber - anchorMaxOffsetForChain(chainID)
	}
	var previousAnchorBlockNumber uint64
	hasPreviousAnchorBlockNumber := false
	for _, anchor := range anchorBlockNumbers {
		if anchor < lastAnchorBlockNumber {
			return fmt.Errorf("anchor %d is below last anchor block number %d", anchor, lastAnchorBlockNumber)
		}
		if anchor < minAnchorBlockNumber || anchor > originBlockNumber {
			return fmt.Errorf("anchor %d is outside valid range [%d, %d]", anchor, minAnchorBlockNumber, originBlockNumber)
		}
		if hasPreviousAnchorBlockNumber && anchor < previousAnchorBlockNumber {
			return fmt.Errorf("anchor %d regressed below previous anchor %d", anchor, previousAnchorBlockNumber)
		}
		previousAnchorBlockNumber = anchor
		hasPreviousAnchorBlockNumber = true
	}
	return nil
}

func anchorMaxOffsetForChain(chainID uint64) uint64 {
	if chainID == shastaManifestMainnetChain {
		return shastaMaxAnchorOffsetMainnet
	}
	return shastaMaxAnchorOffset
}

func validateManifestGasLimit(
	manifest shastaSourceManifest,
	parentBlockNumber uint64,
	parentGasLimit uint64,
) bool {
	effectiveParentGasLimit := effectiveManifestParentGasLimit(parentBlockNumber, parentGasLimit)
	for _, block := range manifest.Blocks {
		lower, upper := manifestGasLimitBounds(effectiveParentGasLimit)
		if block.GasLimit < lower || block.GasLimit > upper {
			return false
		}
		effectiveParentGasLimit = block.GasLimit
	}
	return true
}

func shastaDerivationConfig(chainID uint64, proposalTimestamp uint64) (uint64, int, error) {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return 0, 0, err
	}
	if config.ShastaTime == nil {
		return 0, 0, fmt.Errorf("canonical Shasta fork timestamp missing for chain ID %d", chainID)
	}

	maxBlocks := shastaDerivationSourceMaxBlocks
	if config.IsUnzen(proposalTimestamp) {
		maxBlocks = shastaUnzenDerivationSourceLimit
	}
	return *config.ShastaTime, maxBlocks, nil
}

func manifestGasLimitBounds(parentGasLimit uint64) (uint64, uint64) {
	denominator := new(big.Int).SetUint64(shastaGasLimitDenominator)
	upperMultiplier := new(big.Int).SetUint64(shastaGasLimitDenominator + shastaBlockGasLimitMaxChange)
	upper := new(big.Int).Mul(new(big.Int).SetUint64(parentGasLimit), upperMultiplier)
	upper.Div(upper, denominator)
	maxGas := new(big.Int).SetUint64(shastaMaxBlockGasLimit)
	if upper.Cmp(maxGas) > 0 {
		upper = maxGas
	}

	lowerMultiplier := new(big.Int).SetUint64(shastaGasLimitDenominator - shastaBlockGasLimitMaxChange)
	lower := new(big.Int).Mul(new(big.Int).SetUint64(parentGasLimit), lowerMultiplier)
	lower.Div(lower, denominator)
	minGas := new(big.Int).SetUint64(shastaMinBlockGasLimit)
	if lower.Cmp(minGas) < 0 {
		lower = minGas
	}
	if lower.Cmp(upper) > 0 {
		lower = upper
	}
	return lower.Uint64(), upper.Uint64()
}

func validateManifestBlockBinding(
	ctx context.Context,
	view *GuestInputView,
	proposal shastaProposalView,
	block *types.Block,
	witness *ReplayWitness,
	expected shastaManifestBlock,
	parentHeader *types.Header,
	grandparentHeader *CompactAncestor,
) (anchorV4CheckpointView, error) {
	header := block.Header()
	txs := block.Transactions()
	if len(txs) == 0 {
		return anchorV4CheckpointView{}, fmt.Errorf("missing anchor transaction")
	}
	if err := validateManifestHeaderBaseFee(view.GuestInputChainID, header, parentHeader, grandparentHeader); err != nil {
		return anchorV4CheckpointView{}, err
	}
	if err := validateManifestHeaderDifficulty(view.GuestInputChainID, header); err != nil {
		return anchorV4CheckpointView{}, err
	}
	if err := validateManifestHeaderForkFields(view.GuestInputChainID, header); err != nil {
		return anchorV4CheckpointView{}, err
	}
	if err := validateManifestHeaderStaticFields(header); err != nil {
		return anchorV4CheckpointView{}, err
	}
	if err := validateManifestTransactionRoot(ctx, view, block, witness, expected.Transactions); err != nil {
		return anchorV4CheckpointView{}, err
	}

	if header.Time != expected.Timestamp {
		return anchorV4CheckpointView{}, fmt.Errorf("timestamp mismatch: expected %d got %d", expected.Timestamp, header.Time)
	}
	if header.Coinbase != expected.Coinbase {
		return anchorV4CheckpointView{}, fmt.Errorf("coinbase mismatch: expected %s got %s", expected.Coinbase.Hex(), header.Coinbase.Hex())
	}
	if header.GasLimit != expected.GasLimit+shastaAnchorGasLimit {
		return anchorV4CheckpointView{}, fmt.Errorf("gas limit mismatch: expected %d got %d", expected.GasLimit+shastaAnchorGasLimit, header.GasLimit)
	}
	expectedExtra := encodeShastaManifestExtraData(proposal.BasefeeSharingPctg, view.Taiko.ProposalID)
	if !bytes.Equal(header.Extra, expectedExtra) {
		return anchorV4CheckpointView{}, fmt.Errorf("extra_data mismatch")
	}
	expectedMixHash := shastaManifestMixHash(shastaManifestParentDifficulty(parentHeader), header.Number.Uint64())
	if header.MixDigest != expectedMixHash {
		return anchorV4CheckpointView{}, fmt.Errorf("mix_hash mismatch: expected %s got %s", expectedMixHash.Hex(), header.MixDigest.Hex())
	}

	return validateManifestAnchorTransaction(view, txs[0], header, expected)
}

func validateManifestHeaderDifficulty(chainID uint64, header *types.Header) error {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return err
	}
	if config.IsUnzen(header.Time) {
		return nil
	}
	if header.Difficulty == nil {
		return fmt.Errorf("missing difficulty in pre-Unzen block header")
	}
	if header.Difficulty.Sign() != 0 {
		return fmt.Errorf("pre-Unzen difficulty mismatch: expected 0 got %s", header.Difficulty)
	}
	return nil
}

func validateManifestHeaderForkFields(chainID uint64, header *types.Header) error {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return err
	}
	if header.Number == nil {
		return fmt.Errorf("block header is missing number for fork field validation")
	}
	if config.IsUnzen(header.Time) {
		if err := validateManifestUnzenHeaderFields(header); err != nil {
			return err
		}
	} else if err := validateManifestPreUnzenHeaderFields(header); err != nil {
		return err
	}
	return validateManifestHeaderSlotNumber(config, header)
}

func validateManifestPreUnzenHeaderFields(header *types.Header) error {
	if header.BlobGasUsed != nil {
		return fmt.Errorf("pre-Unzen blob_gas_used must be absent")
	}
	if header.ExcessBlobGas != nil {
		return fmt.Errorf("pre-Unzen excess_blob_gas must be absent")
	}
	if header.ParentBeaconRoot != nil {
		return fmt.Errorf("pre-Unzen parent_beacon_block_root must be absent")
	}
	if header.RequestsHash != nil {
		return fmt.Errorf("pre-Unzen requests_hash must be absent")
	}
	return nil
}

func validateManifestUnzenHeaderFields(header *types.Header) error {
	if header.BlobGasUsed == nil {
		return fmt.Errorf("Unzen blob_gas_used missing")
	}
	if *header.BlobGasUsed != 0 {
		return fmt.Errorf("Unzen blob_gas_used mismatch: expected 0 got %d", *header.BlobGasUsed)
	}
	if header.ExcessBlobGas == nil {
		return fmt.Errorf("Unzen excess_blob_gas missing")
	}
	if *header.ExcessBlobGas != 0 {
		return fmt.Errorf("Unzen excess_blob_gas mismatch: expected 0 got %d", *header.ExcessBlobGas)
	}
	if header.ParentBeaconRoot == nil {
		return fmt.Errorf("Unzen parent_beacon_block_root missing")
	}
	if *header.ParentBeaconRoot != (common.Hash{}) {
		return fmt.Errorf("Unzen parent_beacon_block_root mismatch: expected %s got %s", common.Hash{}.Hex(), header.ParentBeaconRoot.Hex())
	}
	if header.RequestsHash == nil {
		return fmt.Errorf("Unzen requests_hash missing")
	}
	if *header.RequestsHash != types.EmptyRequestsHash {
		return fmt.Errorf("Unzen requests_hash mismatch: expected %s got %s", types.EmptyRequestsHash.Hex(), header.RequestsHash.Hex())
	}
	return nil
}

func validateManifestHeaderSlotNumber(config *params.ChainConfig, header *types.Header) error {
	if config.IsAmsterdam(header.Number, header.Time) {
		if header.SlotNumber == nil {
			return fmt.Errorf("Amsterdam slot_number missing")
		}
		return nil
	}
	if header.SlotNumber != nil {
		return fmt.Errorf("pre-Amsterdam slot_number must be absent")
	}
	return nil
}

func validateManifestHeaderStaticFields(header *types.Header) error {
	if header.Nonce != (types.BlockNonce{}) {
		return fmt.Errorf("block header nonce mismatch: expected 0 got %d", header.Nonce.Uint64())
	}
	if header.UncleHash != types.EmptyUncleHash {
		return fmt.Errorf(
			"ommers_hash mismatch: expected %s got %s",
			types.EmptyUncleHash.Hex(),
			header.UncleHash.Hex(),
		)
	}
	if header.WithdrawalsHash == nil {
		return fmt.Errorf("withdrawals_root missing")
	}
	if *header.WithdrawalsHash != types.EmptyWithdrawalsHash {
		return fmt.Errorf(
			"withdrawals_root mismatch: expected %s got %s",
			types.EmptyWithdrawalsHash.Hex(),
			header.WithdrawalsHash.Hex(),
		)
	}
	return nil
}

func validateManifestHeaderBaseFee(
	chainID uint64,
	header *types.Header,
	parentHeader *types.Header,
	grandparentHeader *CompactAncestor,
) error {
	config, err := chainConfigFor(chainID)
	if err != nil {
		return err
	}
	if !config.IsShasta(header.Time) {
		return nil
	}
	if header.BaseFee == nil {
		return fmt.Errorf("missing base fee per gas in block header")
	}
	if parentHeader == nil {
		return fmt.Errorf("missing parent header for base fee validation")
	}
	if header.Number == nil {
		return fmt.Errorf("block header is missing number for base fee validation")
	}
	if parentHeader.Number == nil {
		return fmt.Errorf("parent header is missing number for base fee validation")
	}
	if err := validateManifestHeaderAncestry(header, parentHeader, grandparentHeader); err != nil {
		return err
	}
	if parentHeader.Number.Sign() != 0 && parentHeader.BaseFee == nil {
		return fmt.Errorf("missing base fee per gas in parent block header")
	}

	parentBlockTime, err := manifestParentBlockTime(parentHeader, grandparentHeader)
	if err != nil {
		return err
	}
	if err := misc.VerifyEIP4396Header(config, parentHeader, parentBlockTime, header); err != nil {
		return err
	}
	return nil
}

func validateManifestHeaderAncestry(
	header *types.Header,
	parentHeader *types.Header,
	grandparentHeader *CompactAncestor,
) error {
	if parentHeader.Number.Uint64()+1 != header.Number.Uint64() {
		return fmt.Errorf(
			"parent header number mismatch: got %d expected %d",
			parentHeader.Number.Uint64(),
			header.Number.Uint64()-1,
		)
	}
	if parentHeader.Hash() != header.ParentHash {
		return fmt.Errorf(
			"parent header hash mismatch: got %s expected %s",
			parentHeader.Hash().Hex(),
			header.ParentHash.Hex(),
		)
	}
	if parentHeader.Number.Sign() == 0 {
		return nil
	}
	if grandparentHeader == nil {
		return fmt.Errorf("missing grandparent header for base fee validation")
	}
	if grandparentHeader.Number+1 != parentHeader.Number.Uint64() {
		return fmt.Errorf(
			"grandparent header number mismatch: got %d expected %d",
			grandparentHeader.Number,
			parentHeader.Number.Uint64()-1,
		)
	}
	if grandparentHeader.Hash != parentHeader.ParentHash {
		return fmt.Errorf(
			"grandparent header hash mismatch: got %s expected %s",
			grandparentHeader.Hash.Hex(),
			parentHeader.ParentHash.Hex(),
		)
	}
	return nil
}

func manifestParentBlockTime(parentHeader *types.Header, grandparentHeader *CompactAncestor) (uint64, error) {
	if parentHeader.Number.Sign() == 0 {
		return 0, nil
	}
	if grandparentHeader == nil {
		return 0, fmt.Errorf("missing grandparent header for base fee validation")
	}
	if parentHeader.Time < grandparentHeader.Timestamp {
		return 0, fmt.Errorf(
			"parent header timestamp is before grandparent: parent=%d grandparent=%d",
			parentHeader.Time,
			grandparentHeader.Timestamp,
		)
	}
	return parentHeader.Time - grandparentHeader.Timestamp, nil
}

func validateManifestAnchorTransaction(
	view *GuestInputView,
	tx *types.Transaction,
	header *types.Header,
	expected shastaManifestBlock,
) (anchorV4CheckpointView, error) {
	if tx.Type() != types.DynamicFeeTxType {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction type mismatch: expected %d got %d", types.DynamicFeeTxType, tx.Type())
	}
	expectedRecipient, err := shastaTaikoL2Address(view.GuestInputChainID)
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	if tx.To() == nil || *tx.To() != expectedRecipient {
		got := "<nil>"
		if tx.To() != nil {
			got = tx.To().Hex()
		}
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction recipient mismatch: expected %s got %s", expectedRecipient.Hex(), got)
	}
	if tx.ChainId().Uint64() != view.GuestInputChainID {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction chain_id mismatch: expected %d got %s", view.GuestInputChainID, tx.ChainId())
	}
	if tx.Value().Sign() != 0 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction value mismatch: expected 0 got %s", tx.Value())
	}
	if tx.Gas() != shastaAnchorGasLimit {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction gas limit mismatch: expected %d got %d", shastaAnchorGasLimit, tx.Gas())
	}
	if header.BaseFee == nil {
		return anchorV4CheckpointView{}, fmt.Errorf("missing base fee per gas in block header")
	}
	if header.Number == nil {
		return anchorV4CheckpointView{}, fmt.Errorf("block header is missing number for anchor transaction validation")
	}
	if tx.GasFeeCap().Cmp(header.BaseFee) != 0 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction max_fee_per_gas mismatch: expected %s got %s", header.BaseFee, tx.GasFeeCap())
	}
	if tx.GasTipCap().Sign() != 0 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction max_priority_fee_per_gas mismatch")
	}
	if len(tx.AccessList()) != 0 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchor transaction access list must be empty")
	}
	config, err := chainConfigFor(view.GuestInputChainID)
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	signer := types.MakeSigner(config, header.Number, header.Time)
	sender, err := types.Sender(signer, tx)
	if err != nil {
		return anchorV4CheckpointView{}, fmt.Errorf("recover anchor transaction sender: %w", err)
	}
	if sender != shastaGoldenTouchAccount {
		return anchorV4CheckpointView{}, fmt.Errorf(
			"anchor transaction sender mismatch: expected %s got %s",
			shastaGoldenTouchAccount.Hex(),
			sender.Hex(),
		)
	}
	if err := validateCanonicalAnchorSignature(tx, signer); err != nil {
		return anchorV4CheckpointView{}, err
	}
	checkpoint, err := decodeAnchorV4Checkpoint(tx.Data())
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	if checkpoint.blockNumber != expected.AnchorBlockNumber {
		return anchorV4CheckpointView{}, fmt.Errorf(
			"anchor checkpoint block number mismatch: expected %d got %d",
			expected.AnchorBlockNumber,
			checkpoint.blockNumber,
		)
	}
	return checkpoint, nil
}

func validateCanonicalAnchorSignature(tx *types.Transaction, signer types.Signer) error {
	// The driver signs GoldenTouch anchor transactions with a fixed-k signer.
	// Sender recovery alone is not enough because an alternate valid signature
	// preserves EVM semantics but changes the transaction and block hashes.
	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   tx.ChainId(),
		Nonce:     tx.Nonce(),
		GasTipCap: tx.GasTipCap(),
		GasFeeCap: tx.GasFeeCap(),
		Gas:       tx.Gas(),
		To:        tx.To(),
		Value:     tx.Value(),
		Data:      tx.Data(),
	})
	signature, err := signShastaAnchorPayloadFixedK(signer.Hash(unsigned).Bytes())
	if err != nil {
		return err
	}
	canonical, err := unsigned.WithSignature(signer, signature)
	if err != nil {
		return fmt.Errorf("sign canonical anchor transaction: %w", err)
	}
	if canonical.Hash() != tx.Hash() {
		return fmt.Errorf(
			"anchor transaction signature mismatch: expected fixed-k GoldenTouch signature hash %s got %s",
			canonical.Hash().Hex(),
			tx.Hash().Hex(),
		)
	}
	return nil
}

func signShastaAnchorPayloadFixedK(hash []byte) ([]byte, error) {
	if len(hash) != common.HashLength {
		return nil, fmt.Errorf("anchor transaction signing hash must be %d bytes, got %d", common.HashLength, len(hash))
	}
	var priv secp256k1.ModNScalar
	if overflow := priv.SetByteSlice(common.FromHex(shastaGoldenTouchPrivateKey)); overflow || priv.IsZero() {
		return nil, fmt.Errorf("invalid GoldenTouch private key")
	}
	for _, k := range []uint32{1, 2} {
		sig, ok := signShastaAnchorPayloadWithK(hash, &priv, new(secp256k1.ModNScalar).SetInt(k))
		if ok {
			return sig, nil
		}
	}
	return nil, fmt.Errorf("failed to sign anchor transaction with fixed k")
}

func signShastaAnchorPayloadWithK(hash []byte, priv *secp256k1.ModNScalar, k *secp256k1.ModNScalar) ([]byte, bool) {
	var kG secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(k, &kG)
	kG.ToAffine()

	r, overflow := fieldToModNScalar(&kG.X)
	pubKeyRecoveryCode := byte(overflow<<1) | byte(kG.Y.IsOddBit())

	kinv := new(secp256k1.ModNScalar).InverseValNonConst(k)
	partialS := new(secp256k1.ModNScalar).Mul2(priv, &r)

	var e secp256k1.ModNScalar
	e.SetByteSlice(hash)
	s := new(secp256k1.ModNScalar).Set(partialS).Add(&e).Mul(kinv)
	if s.IsZero() {
		return nil, false
	}
	if s.IsOverHalfOrder() {
		s.Negate()
		pubKeyRecoveryCode ^= 0x01
	}

	var sig [65]byte
	r.PutBytesUnchecked(sig[:32])
	s.PutBytesUnchecked(sig[32:64])
	sig[64] = pubKeyRecoveryCode
	return sig[:], true
}

func fieldToModNScalar(v *secp256k1.FieldVal) (secp256k1.ModNScalar, uint32) {
	var buf [32]byte
	v.PutBytes(&buf)
	var s secp256k1.ModNScalar
	overflow := s.SetBytes(&buf)
	return s, overflow
}

func shastaTaikoL2Address(chainID uint64) (common.Address, error) {
	return shastaPredeployAddress(chainID, shastaTaikoL2AddressSuffix, "TaikoL2")
}

func shastaCheckpointStoreAddress(chainID uint64) (common.Address, error) {
	return shastaPredeployAddress(chainID, shastaCheckpointStoreSuffix, "CheckpointStore")
}

func shastaPredeployAddress(chainID uint64, suffix string, name string) (common.Address, error) {
	prefix := strings.TrimPrefix(fmt.Sprintf("%d", chainID), "0")
	padding := common.AddressLength*2 - len(prefix) - len(suffix)
	if padding < 0 {
		return common.Address{}, fmt.Errorf("chain_id %d is too long to derive %s address", chainID, name)
	}
	return common.HexToAddress("0x" + prefix + strings.Repeat("0", padding) + suffix), nil
}

type anchorV4CheckpointView struct {
	blockNumber uint64
	blockHash   common.Hash
	stateRoot   common.Hash
}

func decodeAnchorV4Checkpoint(input []byte) (anchorV4CheckpointView, error) {
	selector := crypto.Keccak256([]byte("anchorV4((uint48,bytes32,bytes32))"))[:4]
	if len(input) < 4 || !bytes.Equal(input[:4], selector) {
		return anchorV4CheckpointView{}, fmt.Errorf("first transaction is not anchorV4")
	}
	if len(input) != anchorV4CalldataLength {
		return anchorV4CheckpointView{}, fmt.Errorf(
			"anchorV4 calldata length mismatch: expected %d got %d",
			anchorV4CalldataLength,
			len(input),
		)
	}
	blockNumber := binary.BigEndian.Uint64(input[4+24 : 4+32])
	if blockNumber > maxUint48 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchorV4 blockNumber exceeds uint48")
	}
	return anchorV4CheckpointView{
		blockNumber: blockNumber,
		blockHash:   common.BytesToHash(input[4+32 : 4+64]),
		stateRoot:   common.BytesToHash(input[4+64 : 4+96]),
	}, nil
}

func encodeShastaManifestExtraData(basefeeSharingPctg uint8, proposalID uint64) []byte {
	var out [7]byte
	out[0] = basefeeSharingPctg
	var proposalBytes [8]byte
	binary.BigEndian.PutUint64(proposalBytes[:], proposalID)
	copy(out[1:], proposalBytes[2:])
	return out[:]
}

func shastaManifestMixHash(parentMixHash common.Hash, blockNumber uint64) common.Hash {
	var blockNumberWord [32]byte
	binary.BigEndian.PutUint64(blockNumberWord[24:], blockNumber)
	data := make([]byte, 0, 64)
	data = append(data, parentMixHash.Bytes()...)
	data = append(data, blockNumberWord[:]...)
	return crypto.Keccak256Hash(data)
}

func shastaManifestParentDifficulty(parentHeader *types.Header) common.Hash {
	if parentHeader == nil || parentHeader.Difficulty == nil {
		return common.Hash{}
	}
	return common.BigToHash(parentHeader.Difficulty)
}

func shastaCheckpointStorageSlots(blockNumber uint64) (common.Hash, common.Hash) {
	var buf [64]byte
	new(big.Int).SetUint64(blockNumber).FillBytes(buf[0:32])
	new(big.Int).SetUint64(shastaSignalServiceCheckpointsSlot).FillBytes(buf[32:64])
	blockHashSlot := crypto.Keccak256Hash(buf[:])
	stateRootSlot := common.BigToHash(new(big.Int).Add(blockHashSlot.Big(), big.NewInt(1)))
	return blockHashSlot, stateRootSlot
}

func verifiedParentShastaCheckpoint(view *GuestInputView, blockNumber uint64) (anchorV4CheckpointView, error) {
	store, err := verifiedCheckpointStore(view)
	if err != nil {
		return anchorV4CheckpointView{}, err
	}
	blockHashSlot, stateRootSlot := shastaCheckpointStorageSlots(blockNumber)
	blockHash, err := readParentL2Storage(view, store, blockHashSlot)
	if err != nil {
		return anchorV4CheckpointView{}, fmt.Errorf("read parent CheckpointStore blockHash: %w", err)
	}
	stateRoot, err := readParentL2Storage(view, store, stateRootSlot)
	if err != nil {
		return anchorV4CheckpointView{}, fmt.Errorf("read parent CheckpointStore stateRoot: %w", err)
	}
	if blockHash == (common.Hash{}) {
		return anchorV4CheckpointView{}, fmt.Errorf("parent CheckpointStore blockHash is zero")
	}
	if stateRoot == (common.Hash{}) {
		return anchorV4CheckpointView{}, fmt.Errorf("parent CheckpointStore stateRoot is zero")
	}
	return anchorV4CheckpointView{blockNumber: blockNumber, blockHash: blockHash, stateRoot: stateRoot}, nil
}

func verifiedCheckpointStore(view *GuestInputView) (common.Address, error) {
	expected, err := shastaCheckpointStoreAddress(view.GuestInputChainID)
	if err != nil {
		return common.Address{}, err
	}
	got, err := decodeWitnessCheckpointStore(view)
	if err != nil {
		return common.Address{}, err
	}
	if got != expected {
		return common.Address{}, fmt.Errorf(
			"checkpoint_store_contract mismatch: expected %s got %s",
			expected.Hex(),
			got.Hex(),
		)
	}
	return expected, nil
}

func decodeWitnessCheckpointStore(view *GuestInputView) (common.Address, error) {
	if len(view.Witnesses) == 0 {
		return common.Address{}, fmt.Errorf("guest input must include at least one witness")
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	return requireAddress(fields, "checkpoint_store_contract", "checkpointStoreContract")
}

func decodeGuestInputL1Headers(raw json.RawMessage) (*types.Header, []*types.Header, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("unmarshal taiko: %w", err)
	}
	l1HeaderRaw, ok := lookupField(fields, "l1_header", "l1Header")
	if !ok || isEmptyOrNullRawMessage(l1HeaderRaw) {
		return nil, nil, fmt.Errorf("missing taiko.l1_header")
	}
	l1Header, err := decodeHeader(l1HeaderRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode taiko.l1_header: %w", err)
	}
	ancestorsRaw, ok := lookupField(fields, "l1_ancestor_headers", "l1AncestorHeaders")
	if !ok || isEmptyOrNullRawMessage(ancestorsRaw) {
		return l1Header, nil, nil
	}
	var rawList []json.RawMessage
	if err := json.Unmarshal(ancestorsRaw, &rawList); err != nil {
		return nil, nil, fmt.Errorf("unmarshal taiko.l1_ancestor_headers: %w", err)
	}
	ancestors := make([]*types.Header, len(rawList))
	for i, r := range rawList {
		h, err := decodeHeader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("decode taiko.l1_ancestor_headers[%d]: %w", i, err)
		}
		ancestors[i] = h
	}
	return l1Header, ancestors, nil
}

func decodeGuestInputLastAnchorBlockNumber(raw json.RawMessage) (*uint64, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal taiko: %w", err)
	}
	proverDataRaw, ok := lookupField(fields, "prover_data", "proverData")
	if !ok || isEmptyOrNullRawMessage(proverDataRaw) {
		return nil, nil
	}
	proverData, err := decodeJSONObject(proverDataRaw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal taiko.prover_data: %w", err)
	}
	lastAnchor, err := optionalUint64Ptr(proverData, "last_anchor_block_number", "lastAnchorBlockNumber")
	if err != nil {
		return nil, fmt.Errorf("parse taiko.prover_data.last_anchor_block_number: %w", err)
	}
	return lastAnchor, nil
}

func manifestForcedInclusionPrefixCount(sourceSpans []manifestAnchorSourceSpan) int {
	count := 0
	for _, span := range sourceSpans[:max(0, len(sourceSpans)-1)] {
		if span.isForcedInclusion {
			count += span.blockCount
		}
	}
	return count
}

func validateAnchorL1Linkage(
	view *GuestInputView,
	proposal shastaProposalView,
	checkpoints []anchorV4CheckpointView,
	sourceSpans []manifestAnchorSourceSpan,
	lastAnchor uint64,
) error {
	l1Header, ancestors, err := decodeGuestInputL1Headers(view.TaikoRaw)
	if err != nil {
		return err
	}
	if l1Header.Number == nil || l1Header.Number.Uint64() != proposal.OriginBlockNumber {
		return fmt.Errorf("taiko.l1_header.number mismatch: expected %d", proposal.OriginBlockNumber)
	}
	if l1Header.Hash() != proposal.OriginBlockHash {
		return fmt.Errorf("taiko.l1_header hash mismatch")
	}

	anchorNumbers := make([]uint64, len(checkpoints))
	for i, cp := range checkpoints {
		anchorNumbers[i] = cp.blockNumber
	}
	if shouldBypassStalledAnchorLinkage(anchorNumbers, lastAnchor, proposal.OriginBlockNumber, view.GuestInputChainID) {
		parentCheckpoint, err := verifiedParentShastaCheckpoint(view, lastAnchor)
		if err != nil {
			return err
		}
		for _, cp := range checkpoints {
			if cp != parentCheckpoint {
				return fmt.Errorf("anchor checkpoint (%d) does not match parent checkpoint (%d)",
					cp.blockNumber, parentCheckpoint.blockNumber)
			}
		}
		return nil
	}
	startIndex := manifestForcedInclusionPrefixCount(sourceSpans)
	if startIndex > len(checkpoints) {
		return fmt.Errorf("forced-inclusion prefix exceeds checkpoint count")
	}
	if startIndex > 0 {
		parentCheckpoint, err := verifiedParentShastaCheckpoint(view, lastAnchor)
		if err != nil {
			return err
		}
		for _, cp := range checkpoints[:startIndex] {
			if cp != parentCheckpoint {
				return fmt.Errorf("forced-inclusion anchor checkpoint (%d) does not match parent checkpoint (%d)",
					cp.blockNumber, parentCheckpoint.blockNumber)
			}
		}
	}
	headerCheckpoints := checkpoints[startIndex:]

	if len(ancestors) == 0 {
		return fmt.Errorf("taiko.l1_ancestor_headers must not be empty")
	}
	cpIndex := 0
	var prevNumber *uint64
	var prevHash common.Hash
	var lastNumber uint64
	var lastHash common.Hash
	for i, header := range ancestors {
		if header.Number == nil {
			return fmt.Errorf("taiko.l1_ancestor_headers[%d] missing number", i)
		}
		headerHash := header.Hash()
		number := header.Number.Uint64()
		if prevNumber != nil {
			if number != *prevNumber+1 {
				return fmt.Errorf("taiko.l1_ancestor_headers must be contiguous at index %d", i)
			}
			if header.ParentHash != prevHash {
				return fmt.Errorf("taiko.l1_ancestor_headers parent hash mismatch at index %d", i)
			}
		}
		for cpIndex < len(headerCheckpoints) && headerCheckpoints[cpIndex].blockNumber == number {
			cp := headerCheckpoints[cpIndex]
			if cp.blockHash != headerHash || cp.stateRoot != header.Root {
				return fmt.Errorf(
					"anchor checkpoint (%d) not found in taiko.l1_ancestor_headers", cp.blockNumber)
			}
			cpIndex++
		}
		n := number
		prevNumber = &n
		prevHash = headerHash
		lastNumber = number
		lastHash = headerHash
	}
	if lastNumber != proposal.OriginBlockNumber {
		return fmt.Errorf("taiko.l1_ancestor_headers last block number mismatch: expected %d got %d",
			proposal.OriginBlockNumber, lastNumber)
	}
	if lastHash != proposal.OriginBlockHash {
		return fmt.Errorf("taiko.l1_ancestor_headers last hash mismatch")
	}
	if cpIndex != len(headerCheckpoints) {
		return fmt.Errorf("anchor checkpoint (%d) not found in taiko.l1_ancestor_headers",
			headerCheckpoints[cpIndex].blockNumber)
	}
	return nil
}

func shouldBypassStalledAnchorLinkage(anchorNumbers []uint64, lastAnchor, origin, chainID uint64) bool {
	if len(anchorNumbers) == 0 {
		return false
	}
	first := anchorNumbers[0]
	if first != lastAnchor || origin-first <= anchorMaxOffsetForChain(chainID) {
		return false
	}
	for _, a := range anchorNumbers {
		if a != first {
			return false
		}
	}
	return true
}
