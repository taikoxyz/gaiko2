package prover

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	shastaPayloadVersion       = 1
	shastaAnchorGasLimit       = 1_000_000
	shastaManifestMainnetChain = 167000
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
	AnchorBlockNumber uint64
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

	parentHeader, err := decodeLastFullProposalAncestorHeader(view.Raw.ProposalAncestorHeaders)
	if err != nil {
		return err
	}
	lastAnchor, err := decodeGuestInputLastAnchorBlockNumber(view.TaikoRaw)
	if err != nil {
		return err
	}
	parent := shastaManifestParentContext{
		Header:            parentHeader,
		AnchorBlockNumber: lastAnchor,
	}

	derived := make([]shastaManifestBlock, 0, len(view.Witnesses))
	for sourceIndex, source := range proposal.Sources {
		if len(source.BlobSlice.BlobHashes) > 0 {
			return fmt.Errorf("blob-backed manifest binding is not implemented")
		}

		dataSource, err := decodeBlobSourceData(view.DataSourcesRaw[sourceIndex])
		if err != nil {
			return fmt.Errorf("decode data_sources[%d]: %w", sourceIndex, err)
		}
		manifest := decodeInlineSourceManifest(dataSource, source.BlobSlice.Offset)
		manifest = prepareInlineSourceManifest(manifest, source, parent, proposal, view.GuestInputChainID)

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
	for index, expectedBlock := range derived {
		block, _, err := decodeReplayBlock(view.Witnesses[index].ReplayBlock)
		if err != nil {
			return fmt.Errorf("decode witness block %d: %w", index, err)
		}
		if err := validateManifestBlockBinding(view, proposal, block, expectedBlock, canonicalParent); err != nil {
			return fmt.Errorf("manifest block %d: %w", index, err)
		}
		canonicalParent = block.Header()
	}

	return nil
}

func decodeLastFullProposalAncestorHeader(raws []json.RawMessage) (*types.Header, error) {
	for index := len(raws) - 1; index >= 0; index-- {
		var decoded rawWitnessHeader
		if err := json.Unmarshal(raws[index], &decoded); err != nil {
			continue
		}
		if isEmptyOrNullRawMessage(decoded.Header) {
			continue
		}
		header, err := decodeHeader(decoded.Header)
		if err != nil {
			return nil, fmt.Errorf("decode proposal ancestor header %d: %w", index, err)
		}
		return header, nil
	}
	return nil, fmt.Errorf("missing full proposal ancestor header")
}

func decodeInlineSourceManifest(dataSource blobSourceDataView, offset uint64) shastaSourceManifest {
	if len(dataSource.TxDataFromCalldata) != 0 {
		manifest, err := decodeManifestPayload(dataSource.TxDataFromCalldata, offset)
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
		manifest, err := decodeManifestPayload(concatenated, offset)
		if err == nil {
			return manifest
		}
	}
	return defaultSourceManifest()
}

func decodeManifestPayload(payload []byte, offset uint64) (shastaSourceManifest, error) {
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
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return shastaSourceManifest{}, fmt.Errorf("decompress manifest payload: %w", err)
	}

	var manifest shastaSourceManifest
	if err := rlp.DecodeBytes(decoded, &manifest); err != nil {
		return shastaSourceManifest{}, fmt.Errorf("decode manifest rlp: %w", err)
	}
	return manifest, nil
}

func prepareInlineSourceManifest(
	manifest shastaSourceManifest,
	source shastaDerivationSourceView,
	parent shastaManifestParentContext,
	proposal shastaProposalView,
	chainID uint64,
) shastaSourceManifest {
	if source.IsForcedInclusion && len(manifest.Blocks) != 1 {
		manifest = defaultSourceManifest()
	}
	if len(manifest.Blocks) == 0 || isDefaultSourceManifest(manifest) {
		manifest = defaultSourceManifest()
		applyInheritedManifestMetadata(&manifest, parent, proposal, chainID)
	}
	return manifest
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
) {
	parentTime := parent.Header.Time
	parentGasLimit := effectiveManifestParentGasLimit(parent.Header.Number.Uint64(), parent.Header.GasLimit)
	for index := range manifest.Blocks {
		timestamp := manifestTimestampLowerBound(parentTime, proposal.Timestamp, 0, chainID)
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

func validateManifestBlockBinding(
	view *GuestInputView,
	proposal shastaProposalView,
	block *types.Block,
	expected shastaManifestBlock,
	parentHeader *types.Header,
) error {
	header := block.Header()
	txs := block.Transactions()
	if len(txs) == 0 {
		return fmt.Errorf("missing anchor transaction")
	}
	if len(txs) != len(expected.Transactions)+1 {
		return fmt.Errorf(
			"transaction count mismatch: expected %d got %d",
			len(expected.Transactions)+1,
			len(txs),
		)
	}
	for index, expectedTx := range expected.Transactions {
		expectedBytes, err := expectedTx.MarshalBinary()
		if err != nil {
			return fmt.Errorf("encode expected transaction %d: %w", index+1, err)
		}
		actualBytes, err := txs[index+1].MarshalBinary()
		if err != nil {
			return fmt.Errorf("encode actual transaction %d: %w", index+1, err)
		}
		if !bytes.Equal(expectedBytes, actualBytes) {
			return fmt.Errorf("transaction %d mismatch", index+1)
		}
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
	expectedMixHash := shastaManifestMixHash(parentHeader.MixDigest, header.Number.Uint64())
	if header.MixDigest != expectedMixHash {
		return fmt.Errorf("mix_hash mismatch: expected %s got %s", expectedMixHash.Hex(), header.MixDigest.Hex())
	}

	return validateManifestAnchorTransaction(view, txs[0], header, expected)
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
