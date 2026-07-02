package prover

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"

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
)

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

func ValidateGuestInputManifestBinding(view *GuestInputView) error {
	if view == nil {
		return fmt.Errorf("guest input view is nil")
	}

	proposal, err := decodeGuestInputTaikoProposal(view.TaikoRaw)
	if err != nil {
		return err
	}
	if len(proposal.Sources) == 0 {
		return nil
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
	lastAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
	if err != nil {
		return err
	}
	parent := shastaManifestParentContext{
		Header:            parentHeader,
		GrandparentHeader: grandparentHeader,
		AnchorBlockNumber: lastAnchor,
	}
	forkTimestamp, err := decodeWitnessForkTimestamp(view, "SHASTA")
	if err != nil {
		return err
	}
	maxBlocks, err := derivationSourceMaxBlocks(view, proposal)
	if err != nil {
		return err
	}

	derived := make([]shastaManifestBlock, 0, len(view.Witnesses))
	for sourceIndex, source := range proposal.Sources {
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

	canonicalParent := parentHeader
	canonicalGrandparent := grandparentHeader
	for index, expectedBlock := range derived {
		block, witness, err := decodeReplayBlock(view.Witnesses[index].ReplayBlock)
		if err != nil {
			return fmt.Errorf("decode witness block %d: %w", index, err)
		}
		if err := validateManifestBlockBinding(view, proposal, block, witness, expectedBlock, canonicalParent, canonicalGrandparent); err != nil {
			return fmt.Errorf("manifest block %d: %w", index, err)
		}
		rolledGrandparent := compactAncestorFromHeader(canonicalParent)
		canonicalGrandparent = &rolledGrandparent
		canonicalParent = block.Header()
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
	if len(source.BlobSlice.BlobHashes) == 0 {
		manifest = decodeInlineSourceManifest(dataSource, source.BlobSlice.Offset, maxBlocks)
	} else if source.BlobSlice.Offset > shastaMaxManifestOffset {
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

func decodeInlineSourceManifest(dataSource blobSourceDataView, offset uint64, maxBlocks int) shastaSourceManifest {
	if len(dataSource.TxDataFromCalldata) != 0 {
		manifest, err := decodeManifestPayload(dataSource.TxDataFromCalldata, offset, maxBlocks)
		if err == nil {
			return manifest
		}
		return defaultSourceManifest()
	}
	if len(dataSource.TxDataFromBlob) != 0 {
		var concatenated []byte
		for _, chunk := range dataSource.TxDataFromBlob {
			concatenated = append(concatenated, chunk...)
		}
		manifest, err := decodeManifestPayload(concatenated, offset, maxBlocks)
		if err == nil {
			return manifest
		}
	}
	return defaultSourceManifest()
}

func decodeBlobBackedSourceManifest(dataSource blobSourceDataView, offset uint64, maxBlocks int) (shastaSourceManifest, error) {
	if len(dataSource.TxDataFromBlob) == 0 {
		return shastaSourceManifest{}, fmt.Errorf("blob-backed derivation source is missing blob data")
	}

	var concatenated []byte
	for index, raw := range dataSource.TxDataFromBlob {
		decoded, err := decodeShastaBlob(raw)
		if err != nil {
			if len(raw) != shastaBytesPerBlob {
				return shastaSourceManifest{}, fmt.Errorf("blob %d has invalid length %d; expected %d", index, len(raw), shastaBytesPerBlob)
			}
			return shastaSourceManifest{}, fmt.Errorf("invalid blob encoding")
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
	parentAnchor := parentAnchorBlockNumber
	highestAnchor := parentAnchorBlockNumber
	maxOffset := uint64(shastaMaxAnchorOffset)
	if chainID == shastaManifestMainnetChain {
		maxOffset = shastaMaxAnchorOffsetMainnet
	}

	for _, block := range manifest.Blocks {
		anchor := block.AnchorBlockNumber
		if anchor < parentAnchor || anchor > originBlockNumber {
			return false
		}
		if originBlockNumber > maxOffset && anchor < originBlockNumber-maxOffset {
			return false
		}
		if anchor > highestAnchor {
			highestAnchor = anchor
		}
		parentAnchor = anchor
	}

	return isForcedInclusion || highestAnchor > parentAnchorBlockNumber
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

func decodeWitnessForkTimestamp(view *GuestInputView, forkName string) (uint64, error) {
	raw, ok, err := lookupWitnessFork(view, forkName)
	if err != nil || !ok {
		return 0, err
	}
	fields, ok, err := decodeForkConditionObject(raw)
	if err != nil || !ok {
		return 0, err
	}
	timestamp, err := optionalUint64Ptr(fields, "Timestamp", "timestamp")
	if err != nil {
		return 0, fmt.Errorf("parse witness.chain_spec.hard_forks.%s.Timestamp: %w", forkName, err)
	}
	if timestamp == nil {
		return 0, nil
	}
	return *timestamp, nil
}

func derivationSourceMaxBlocks(view *GuestInputView, proposal shastaProposalView) (int, error) {
	active, err := witnessForkActiveAt(view, "UNZEN", proposal.Timestamp)
	if err != nil {
		return 0, err
	}
	if active {
		return shastaUnzenDerivationSourceLimit, nil
	}
	return shastaDerivationSourceMaxBlocks, nil
}

func witnessForkActiveAt(view *GuestInputView, forkName string, timestamp uint64) (bool, error) {
	raw, ok, err := lookupWitnessFork(view, forkName)
	if err != nil || !ok {
		return false, err
	}
	fields, ok, err := decodeForkConditionObject(raw)
	if err != nil || !ok {
		return false, err
	}
	forkTimestamp, err := optionalUint64Ptr(fields, "Timestamp", "timestamp")
	if err != nil {
		return false, fmt.Errorf("parse witness.chain_spec.hard_forks.%s.Timestamp: %w", forkName, err)
	}
	if forkTimestamp != nil {
		return timestamp >= *forkTimestamp, nil
	}
	block, err := optionalUint64Ptr(fields, "Block", "block")
	if err != nil {
		return false, fmt.Errorf("parse witness.chain_spec.hard_forks.%s.Block: %w", forkName, err)
	}
	return block != nil && *block == 0, nil
}

func lookupWitnessFork(view *GuestInputView, forkName string) (json.RawMessage, bool, error) {
	if view == nil || len(view.Witnesses) == 0 {
		return nil, false, fmt.Errorf("guest input must include at least one witness")
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return nil, false, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	rawHardForks, ok := lookupField(fields, "hard_forks", "hardForks")
	if !ok {
		return nil, false, nil
	}
	hardForks, err := decodeJSONObject(rawHardForks)
	if err != nil {
		return nil, false, fmt.Errorf("unmarshal witness.chain_spec.hard_forks: %w", err)
	}
	rawFork, ok := lookupForkCaseInsensitive(hardForks, forkName)
	return rawFork, ok, nil
}

func decodeForkConditionObject(raw json.RawMessage) (map[string]json.RawMessage, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.EqualFold(text, "Tbd") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("unknown hard fork string %q", text)
	}
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, false, err
	}
	return fields, true, nil
}

func validateManifestBlockBinding(
	view *GuestInputView,
	proposal shastaProposalView,
	block *types.Block,
	witness *ReplayWitness,
	expected shastaManifestBlock,
	parentHeader *types.Header,
	grandparentHeader *CompactAncestor,
) error {
	header := block.Header()
	txs := block.Transactions()
	if len(txs) == 0 {
		return fmt.Errorf("missing anchor transaction")
	}
	if err := validateManifestHeaderBaseFee(view.GuestInputChainID, header, parentHeader, grandparentHeader); err != nil {
		return err
	}
	if err := validateManifestTransactionRoot(view, block, witness, expected.Transactions); err != nil {
		return err
	}

	if header.Time != expected.Timestamp {
		return fmt.Errorf("timestamp mismatch: expected %d got %d", expected.Timestamp, header.Time)
	}
	if header.Coinbase != expected.Coinbase {
		return fmt.Errorf("coinbase mismatch: expected %s got %s", expected.Coinbase.Hex(), header.Coinbase.Hex())
	}
	if header.GasLimit != expected.GasLimit+shastaAnchorGasLimit {
		return fmt.Errorf("gas limit mismatch: expected %d got %d", expected.GasLimit+shastaAnchorGasLimit, header.GasLimit)
	}
	expectedExtra := encodeShastaManifestExtraData(proposal.BasefeeSharingPctg, view.Taiko.ProposalID)
	if !bytes.Equal(header.Extra, expectedExtra) {
		return fmt.Errorf("extra_data mismatch")
	}
	expectedMixHash := shastaManifestMixHash(shastaManifestParentDifficulty(parentHeader), header.Number.Uint64())
	if header.MixDigest != expectedMixHash {
		return fmt.Errorf("mix_hash mismatch: expected %s got %s", expectedMixHash.Hex(), header.MixDigest.Hex())
	}

	return validateManifestAnchorTransaction(view, txs[0], header, expected)
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
) error {
	expectedRecipient, err := decodeWitnessL2Contract(view)
	if err != nil {
		return err
	}
	if tx.To() == nil || *tx.To() != expectedRecipient {
		got := "<nil>"
		if tx.To() != nil {
			got = tx.To().Hex()
		}
		return fmt.Errorf("anchor transaction recipient mismatch: expected %s got %s", expectedRecipient.Hex(), got)
	}
	if tx.ChainId().Uint64() != view.GuestInputChainID {
		return fmt.Errorf("anchor transaction chain_id mismatch: expected %d got %s", view.GuestInputChainID, tx.ChainId())
	}
	if header.BaseFee == nil {
		return fmt.Errorf("missing base fee per gas in block header")
	}
	if tx.GasFeeCap().Cmp(header.BaseFee) != 0 {
		return fmt.Errorf("anchor transaction max_fee_per_gas mismatch: expected %s got %s", header.BaseFee, tx.GasFeeCap())
	}
	if tx.GasTipCap().Sign() != 0 {
		return fmt.Errorf("anchor transaction max_priority_fee_per_gas mismatch")
	}
	if len(tx.AccessList()) != 0 {
		return fmt.Errorf("anchor transaction access list must be empty")
	}
	checkpoint, err := decodeAnchorV4Checkpoint(tx.Data())
	if err != nil {
		return err
	}
	if checkpoint.blockNumber != expected.AnchorBlockNumber {
		return fmt.Errorf(
			"anchor checkpoint block number mismatch: expected %d got %d",
			expected.AnchorBlockNumber,
			checkpoint.blockNumber,
		)
	}
	return nil
}

func decodeWitnessL2Contract(view *GuestInputView) (common.Address, error) {
	if len(view.Witnesses) == 0 {
		return common.Address{}, fmt.Errorf("guest input must include at least one witness")
	}
	fields, err := decodeJSONObject(view.Witnesses[0].ChainSpecRaw)
	if err != nil {
		return common.Address{}, fmt.Errorf("unmarshal witness.chain_spec: %w", err)
	}
	l2Contract, err := requireAddress(fields, "l2_contract", "l2Contract")
	if err != nil {
		return common.Address{}, fmt.Errorf("parse witness.chain_spec.l2_contract: %w", err)
	}
	return l2Contract, nil
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
	if len(input) < 4+96 {
		return anchorV4CheckpointView{}, fmt.Errorf("anchorV4 calldata too short")
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

func decodeGuestInputLastAnchorBlockNumber(raw json.RawMessage) (uint64, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return 0, fmt.Errorf("unmarshal taiko: %w", err)
	}
	proverDataRaw, ok := lookupField(fields, "prover_data", "proverData")
	if !ok || isEmptyOrNullRawMessage(proverDataRaw) {
		return 0, nil
	}
	proverData, err := decodeJSONObject(proverDataRaw)
	if err != nil {
		return 0, fmt.Errorf("unmarshal taiko.prover_data: %w", err)
	}
	lastAnchor, err := optionalUint64Ptr(proverData, "last_anchor_block_number", "lastAnchorBlockNumber")
	if err != nil {
		return 0, fmt.Errorf("parse taiko.prover_data.last_anchor_block_number: %w", err)
	}
	if lastAnchor == nil {
		return 0, nil
	}
	return *lastAnchor, nil
}
