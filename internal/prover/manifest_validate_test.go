package prover

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/taikoxyz/gaiko2/internal/protocol"
)

func TestValidateManifestBindingAcceptsInlineCalldataSource(t *testing.T) {
	view := newManifestBindingFixture(t).view(t)

	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate manifest binding: %v", err)
	}
}

func TestValidateManifestBindingAcceptsBlobBackedSource(t *testing.T) {
	fixture := newManifestBindingFixture(t)
	fixture.blobBacked = true
	view := fixture.view(t)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate blob source hashes: %v", err)
	}
	if err := ValidateGuestInputManifestBinding(view); err != nil {
		t.Fatalf("validate blob-backed manifest binding: %v", err)
	}
}

func TestValidateManifestBindingRejectsMismatches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*manifestBindingFixture)
		wantErr string
	}{
		{
			name: "derived block count",
			mutate: func(f *manifestBindingFixture) {
				f.addManifestBlock = true
			},
			wantErr: "derived manifest block count mismatch",
		},
		{
			name: "non-anchor transaction",
			mutate: func(f *manifestBindingFixture) {
				f.blockUserTxJSON = manifestUserTxJSON(t, f.chainID, 8, testAddress("77"))
			},
			wantErr: "transaction 1 mismatch",
		},
		{
			name: "missing anchor transaction",
			mutate: func(f *manifestBindingFixture) {
				f.omitAnchorTx = true
				f.omitUserTx = true
			},
			wantErr: "missing anchor transaction",
		},
		{
			name: "timestamp",
			mutate: func(f *manifestBindingFixture) {
				f.blockTimestamp++
			},
			wantErr: "timestamp mismatch",
		},
		{
			name: "coinbase",
			mutate: func(f *manifestBindingFixture) {
				f.blockCoinbase = common.HexToAddress(testAddress("99"))
			},
			wantErr: "coinbase mismatch",
		},
		{
			name: "gas limit",
			mutate: func(f *manifestBindingFixture) {
				f.blockGasLimit++
			},
			wantErr: "gas limit mismatch",
		},
		{
			name: "extra data",
			mutate: func(f *manifestBindingFixture) {
				f.blockExtra = []byte{0xaa}
			},
			wantErr: "extra_data mismatch",
		},
		{
			name: "mix hash",
			mutate: func(f *manifestBindingFixture) {
				f.blockMixDigest = common.HexToHash(testHash("98"))
			},
			wantErr: "mix_hash mismatch",
		},
		{
			name: "anchor recipient",
			mutate: func(f *manifestBindingFixture) {
				f.anchorTo = common.HexToAddress(testAddress("97"))
			},
			wantErr: "anchor transaction recipient mismatch",
		},
		{
			name: "anchor checkpoint",
			mutate: func(f *manifestBindingFixture) {
				f.anchorBlockNumber++
			},
			wantErr: "anchor checkpoint block number mismatch",
		},
		{
			name: "invalid blob encoding",
			mutate: func(f *manifestBindingFixture) {
				f.blobBacked = true
				f.corruptBlobEncoding = true
			},
			wantErr: "invalid blob encoding",
		},
		{
			name: "invalid manifest metadata defaults instead of binding malicious metadata",
			mutate: func(f *manifestBindingFixture) {
				f.manifestTimestamp = f.parentHeader.Time
				f.manifestCoinbase = common.HexToAddress(testAddress("99"))
				f.anchorBlockNumber = 899
				f.blockCoinbase = f.proposer
				f.omitUserTx = true
			},
			wantErr: "",
		},
		{
			name: "forced inclusion inherits metadata and preserves transactions",
			mutate: func(f *manifestBindingFixture) {
				f.isForcedInclusion = true
				f.manifestTimestamp = 1
				f.manifestCoinbase = common.HexToAddress(testAddress("88"))
				f.manifestGasLimit = 10_000_000
				f.anchorBlockNumber = 899
				f.blockCoinbase = f.proposer
			},
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newManifestBindingFixture(t)
			tc.mutate(fixture)
			view := fixture.view(t)

			err := ValidateGuestInputManifestBinding(view)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateGuestInputBlobSourcesAcceptsInlineCalldataSource(t *testing.T) {
	view := newManifestBindingFixture(t).view(t)

	if err := ValidateGuestInputBlobSources(view); err != nil {
		t.Fatalf("validate inline calldata source: %v", err)
	}
}

type manifestBindingFixture struct {
	chainID             uint64
	proposalID          uint64
	proposalTimestamp   uint64
	proposer            common.Address
	originBlockNumber   uint64
	parentHeader        *types.Header
	manifestTimestamp   uint64
	manifestCoinbase    common.Address
	manifestGasLimit    uint64
	manifestUserTxJSON  json.RawMessage
	blockUserTxJSON     json.RawMessage
	blockTimestamp      uint64
	blockCoinbase       common.Address
	blockGasLimit       uint64
	blockExtra          []byte
	blockMixDigest      common.Hash
	blockBaseFee        uint64
	l2Contract          common.Address
	anchorTo            common.Address
	anchorBlockNumber   uint64
	omitAnchorTx        bool
	omitUserTx          bool
	addManifestBlock    bool
	blobBacked          bool
	corruptBlobEncoding bool
	isForcedInclusion   bool
}

func newManifestBindingFixture(t *testing.T) *manifestBindingFixture {
	t.Helper()

	chainID := uint64(167013)
	proposalID := uint64(12345)
	parentMixDigest := common.HexToHash(testHash("91"))
	parentHeader := &types.Header{
		ParentHash:  common.HexToHash(testHash("90")),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    common.HexToAddress(testAddress("10")),
		Root:        common.HexToHash(testHash("11")),
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(41),
		GasLimit:    31_000_000,
		GasUsed:     0,
		Time:        1_000,
		Extra:       []byte{},
		MixDigest:   parentMixDigest,
		Nonce:       types.BlockNonce{},
		BaseFee:     big.NewInt(1),
	}
	manifestTx := manifestUserTxJSON(t, chainID, 7, testAddress("33"))
	manifestGasLimit := uint64(30_000_000)
	blockNumber := uint64(42)

	return &manifestBindingFixture{
		chainID:            chainID,
		proposalID:         proposalID,
		proposalTimestamp:  1_100,
		proposer:           common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		originBlockNumber:  1_000,
		parentHeader:       parentHeader,
		manifestTimestamp:  1_001,
		manifestCoinbase:   common.HexToAddress(testAddress("22")),
		manifestGasLimit:   manifestGasLimit,
		manifestUserTxJSON: manifestTx,
		blockUserTxJSON:    manifestTx,
		blockTimestamp:     1_001,
		blockCoinbase:      common.HexToAddress(testAddress("22")),
		blockGasLimit:      manifestGasLimit + 1_000_000,
		blockExtra:         manifestExtraData(42, proposalID),
		blockMixDigest:     manifestMixHash(parentMixDigest, blockNumber),
		blockBaseFee:       1_000,
		l2Contract:         common.HexToAddress(testAddress("44")),
		anchorTo:           common.HexToAddress(testAddress("44")),
		anchorBlockNumber:  900,
	}
}

func (f *manifestBindingFixture) view(t *testing.T) *GuestInputView {
	t.Helper()

	manifestPayload := f.manifestPayload(t)
	dataSourceJSON, sourceJSON := f.dataSourceAndSourceJSON(t, manifestPayload)
	proposalJSON := f.proposalJSON(t, sourceJSON)
	block := f.blockJSON(t)
	blockHash := replayBlockHash(t, block)
	parentHash := f.parentHeader.Hash().Hex()
	stateRoot := common.HexToHash(testHash("55")).Hex()

	input := protocol.ShastaGuestInput{
		Witnesses: []json.RawMessage{
			mustRawMessage(t, fmt.Sprintf(`{
				"block": %s,
				"chain_spec": %s,
				"witness": {"state": [], "state_indices": [], "codes": [], "headers": [%s]},
				"accounts": {}
			}`, block, f.chainSpecJSON(t), f.parentWitnessHeaderJSON(t))),
		},
		Taiko: mustRawMessage(t, fmt.Sprintf(`{
			"chain_spec": {"chain_id": %d},
			"proposal_id": %d,
			"proposal_event": {"proposal": %s},
			"prover_data": {
				"actual_prover": %q,
				"last_anchor_block_number": 899
			},
			"data_sources": [%s]
		}`, f.chainID, f.proposalID, proposalJSON, testAddress("77"), dataSourceJSON)),
		ProposalAncestorHeaders: []json.RawMessage{mustRawMessage(t, f.parentWitnessHeaderJSON(t))},
		ProofCarryData: f.proofCarryData(
			t,
			parentHash,
			blockHash,
			stateRoot,
			proposalJSON,
		),
	}

	view, err := DecodeGuestInput(input)
	if err != nil {
		t.Fatalf("decode guest input: %v", err)
	}
	return view
}

func (f *manifestBindingFixture) proofCarryData(
	t *testing.T,
	parentHash string,
	blockHash string,
	stateRoot string,
	proposalRaw json.RawMessage,
) json.RawMessage {
	t.Helper()

	proposal, err := decodeShastaProposal(proposalRaw)
	if err != nil {
		t.Fatalf("decode proposal for carry: %v", err)
	}
	proposalHash, err := hashShastaProposal(proposal)
	if err != nil {
		t.Fatalf("hash proposal for carry: %v", err)
	}
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"verifier": %q,
		"transition_input": {
			"proposal_id": %d,
			"proposal_hash": %q,
			"parent_proposal_hash": %q,
			"parent_block_hash": %q,
			"actual_prover": %q,
			"transition": {
				"proposer": %q,
				"timestamp": %d
			},
			"checkpoint": {
				"blockNumber": "0x2a",
				"blockHash": %q,
				"stateRoot": %q
			}
		}
	}`,
		f.chainID,
		testAddress("f9"),
		proposal.ID,
		proposalHash.Hex(),
		proposal.ParentProposalHash.Hex(),
		parentHash,
		testAddress("77"),
		proposal.Proposer.Hex(),
		proposal.Timestamp,
		blockHash,
		stateRoot,
	))
}

func (f *manifestBindingFixture) dataSourceAndSourceJSON(t *testing.T, manifestPayload []byte) (string, string) {
	t.Helper()

	if !f.blobBacked {
		return fmt.Sprintf(`{"tx_data_from_calldata": %q}`, "0x"+hex.EncodeToString(manifestPayload)),
			fmt.Sprintf(`{
				"isForcedInclusion": %t,
				"blobSlice": {
					"blobHashes": [],
					"offset": 0,
					"timestamp": %d
				}
			}`, f.isForcedInclusion, f.proposalTimestamp)
	}

	blob := encodeTestKonaBlob(t, manifestPayload)
	if f.corruptBlobEncoding {
		blob[len(blob)-1] = 0x01
	}
	_, blobHash := testBlobCommitmentAndHash(t, blob)
	return fmt.Sprintf(`{"tx_data_from_blob": [%s]}`, hexStringJSON(blob)),
		fmt.Sprintf(`{
			"isForcedInclusion": %t,
			"blobSlice": {
				"blobHashes": [%q],
				"offset": 0,
				"timestamp": %d
			}
		}`, f.isForcedInclusion, blobHash, f.proposalTimestamp)
}

func (f *manifestBindingFixture) manifestPayload(t *testing.T) []byte {
	t.Helper()

	userTx := decodeTestTransaction(t, f.manifestUserTxJSON)
	blocks := []testManifestBlock{{
		Timestamp:         f.manifestTimestamp,
		Coinbase:          f.manifestCoinbase,
		AnchorBlockNumber: 900,
		GasLimit:          f.manifestGasLimit,
		Transactions:      types.Transactions{userTx},
	}}
	if f.addManifestBlock {
		blocks = append(blocks, testManifestBlock{
			Timestamp:         f.manifestTimestamp + 1,
			Coinbase:          f.manifestCoinbase,
			AnchorBlockNumber: 901,
			GasLimit:          f.manifestGasLimit,
			Transactions:      types.Transactions{},
		})
	}
	return encodeTestManifestPayload(t, testDerivationSourceManifest{Blocks: blocks})
}

func (f *manifestBindingFixture) blockJSON(t *testing.T) json.RawMessage {
	t.Helper()

	txs := []json.RawMessage{}
	if !f.omitAnchorTx {
		txs = append(txs, f.anchorTxJSON(t))
	}
	if !f.omitUserTx {
		txs = append(txs, f.blockUserTxJSON)
	}
	rawTxs, err := json.Marshal(txs)
	if err != nil {
		t.Fatalf("marshal transactions: %v", err)
	}

	header := fmt.Sprintf(`{
		"parentHash": %q,
		"sha3Uncles": %q,
		"miner": %q,
		"stateRoot": %q,
		"transactionsRoot": %q,
		"receiptsRoot": %q,
		"logsBloom": %q,
		"difficulty": "0x0",
		"number": "0x2a",
		"gasLimit": "0x%x",
		"gasUsed": "0x0",
		"timestamp": "0x%x",
		"extraData": %q,
		"mixHash": %q,
		"nonce": "0x0000000000000000",
		"baseFeePerGas": "0x%x"
	}`,
		f.parentHeader.Hash().Hex(),
		types.EmptyUncleHash.Hex(),
		f.blockCoinbase.Hex(),
		testHash("55"),
		testHash("56"),
		testHash("57"),
		testBloom(),
		f.blockGasLimit,
		f.blockTimestamp,
		"0x"+hex.EncodeToString(f.blockExtra),
		f.blockMixDigest.Hex(),
		f.blockBaseFee,
	)

	return mustRawMessage(t, fmt.Sprintf(`{
		"header": %s,
		"body": {
			"transactions": %s,
			"ommers": [],
			"withdrawals": []
		}
	}`, header, rawTxs))
}

func (f *manifestBindingFixture) anchorTxJSON(t *testing.T) json.RawMessage {
	t.Helper()
	input := anchorInput(t, f.anchorBlockNumber, common.HexToHash(testHash("61")), common.HexToHash(testHash("62")))
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x1", "s": "0x1", "yParity": "0x0"},
		"transaction": {
			"Eip1559": {
				"chain_id": "0x%x",
				"nonce": "0x0",
				"max_priority_fee_per_gas": "0x0",
				"max_fee_per_gas": "0x%x",
				"gas": "0xf4240",
				"to": %q,
				"value": "0x0",
				"input": %q,
				"access_list": []
			}
		}
	}`, f.chainID, f.blockBaseFee, f.anchorTo.Hex(), "0x"+hex.EncodeToString(input)))
}

func (f *manifestBindingFixture) chainSpecJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"chain_id": %d,
		"l2_contract": %q,
		"hard_forks": {"SHASTA": {"Block": 0}},
		"verifier_address_forks": {"SHASTA": {"SgxGeth": %q}}
	}`, f.chainID, f.l2Contract.Hex(), testAddress("f9")))
}

func (f *manifestBindingFixture) parentWitnessHeaderJSON(t *testing.T) string {
	t.Helper()
	header := headerJSON(t, f.parentHeader)
	return fmt.Sprintf(`{"header": %s, "hash": %q}`, header, f.parentHeader.Hash().Hex())
}

func (f *manifestBindingFixture) proposalJSON(t *testing.T, sourceJSON string) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"id": %d,
		"timestamp": %d,
		"endOfSubmissionWindowTimestamp": %d,
		"proposer": %q,
		"parentProposalHash": %q,
		"originBlockNumber": %d,
		"originBlockHash": %q,
		"basefeeSharingPctg": 42,
		"sources": [%s]
	}`,
		f.proposalID,
		f.proposalTimestamp,
		f.proposalTimestamp+1_000,
		f.proposer.Hex(),
		testHash("ab"),
		f.originBlockNumber,
		testHash("cd"),
		sourceJSON,
	))
}

type testDerivationSourceManifest struct {
	Blocks []testManifestBlock
}

type testManifestBlock struct {
	Timestamp         uint64
	Coinbase          common.Address
	AnchorBlockNumber uint64
	GasLimit          uint64
	Transactions      types.Transactions
}

func encodeTestManifestPayload(t *testing.T, manifest testDerivationSourceManifest) []byte {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(manifest)
	if err != nil {
		t.Fatalf("rlp encode manifest: %v", err)
	}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(encoded); err != nil {
		t.Fatalf("zlib write manifest: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close manifest: %v", err)
	}

	payload := make([]byte, 64, 64+compressed.Len())
	payload[31] = 1
	binary.BigEndian.PutUint64(payload[56:64], uint64(compressed.Len()))
	payload = append(payload, compressed.Bytes()...)
	return payload
}

func manifestUserTxJSON(t *testing.T, chainID uint64, nonce uint64, to string) json.RawMessage {
	t.Helper()
	return mustRawMessage(t, fmt.Sprintf(`{
		"signature": {"r": "0x2", "s": "0x3", "yParity": "0x0"},
		"transaction": {
			"Eip1559": {
				"chain_id": "0x%x",
				"nonce": "0x%x",
				"max_priority_fee_per_gas": "0x1",
				"max_fee_per_gas": "0x2",
				"gas": "0x5208",
				"to": %q,
				"value": "0x0",
				"input": "0x1234",
				"access_list": []
			}
		}
	}`, chainID, nonce, to))
}

func decodeTestTransaction(t *testing.T, raw json.RawMessage) *types.Transaction {
	t.Helper()
	tx, err := decodeTransaction(raw)
	if err != nil {
		t.Fatalf("decode test transaction: %v", err)
	}
	return tx
}

func anchorInput(t *testing.T, blockNumber uint64, blockHash common.Hash, stateRoot common.Hash) []byte {
	t.Helper()
	tuple, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "blockNumber", Type: "uint48"},
		{Name: "blockHash", Type: "bytes32"},
		{Name: "stateRoot", Type: "bytes32"},
	})
	if err != nil {
		t.Fatalf("anchor tuple ABI: %v", err)
	}
	args := abi.Arguments{{Type: tuple}}
	encoded, err := args.Pack(struct {
		BlockNumber *big.Int
		BlockHash   [32]byte
		StateRoot   [32]byte
	}{
		BlockNumber: new(big.Int).SetUint64(blockNumber),
		BlockHash:   blockHash,
		StateRoot:   stateRoot,
	})
	if err != nil {
		t.Fatalf("anchor calldata ABI pack: %v", err)
	}
	selector := crypto.Keccak256([]byte("anchorV4((uint48,bytes32,bytes32))"))[:4]
	return append(append([]byte{}, selector...), encoded...)
}

func manifestExtraData(basefeeSharingPctg uint8, proposalID uint64) []byte {
	var out [7]byte
	out[0] = basefeeSharingPctg
	var proposalBytes [8]byte
	binary.BigEndian.PutUint64(proposalBytes[:], proposalID)
	copy(out[1:], proposalBytes[2:])
	return out[:]
}

func manifestMixHash(parentMix common.Hash, blockNumber uint64) common.Hash {
	var blockWord [32]byte
	binary.BigEndian.PutUint64(blockWord[24:], blockNumber)
	return crypto.Keccak256Hash(append(parentMix.Bytes(), blockWord[:]...))
}

func headerJSON(t *testing.T, header *types.Header) string {
	t.Helper()
	baseFee := "null"
	if header.BaseFee != nil {
		baseFee = fmt.Sprintf("%q", "0x"+header.BaseFee.Text(16))
	}
	raw := fmt.Sprintf(`{
		"parentHash": %q,
		"sha3Uncles": %q,
		"miner": %q,
		"stateRoot": %q,
		"transactionsRoot": %q,
		"receiptsRoot": %q,
		"logsBloom": %q,
		"difficulty": "0x%s",
		"number": "0x%x",
		"gasLimit": "0x%x",
		"gasUsed": "0x%x",
		"timestamp": "0x%x",
		"extraData": %q,
		"mixHash": %q,
		"nonce": "0x%016x",
		"baseFeePerGas": %s
	}`,
		header.ParentHash.Hex(),
		header.UncleHash.Hex(),
		header.Coinbase.Hex(),
		header.Root.Hex(),
		header.TxHash.Hex(),
		header.ReceiptHash.Hex(),
		"0x"+hex.EncodeToString(header.Bloom.Bytes()),
		header.Difficulty.Text(16),
		header.Number.Uint64(),
		header.GasLimit,
		header.GasUsed,
		header.Time,
		"0x"+hex.EncodeToString(header.Extra),
		header.MixDigest.Hex(),
		binary.BigEndian.Uint64(header.Nonce[:]),
		baseFee,
	)
	return raw
}

func encodeTestKonaBlob(t *testing.T, payload []byte) []byte {
	t.Helper()

	const (
		testBytesPerBlob       = 131072
		testBlobEncodingRounds = 1024
		testBlobMaxDataSize    = (4*31+3)*1024 - 4
	)
	if len(payload) > testBlobMaxDataSize {
		t.Fatalf("test payload too large for one blob: %d", len(payload))
	}

	blob := make([]byte, testBytesPerBlob)
	readOffset := 0
	writeOffset := 0

	write1 := func(value byte) {
		if value&0xc0 != 0 {
			t.Fatalf("test encoder invalid 6-bit value: %x", value)
		}
		if writeOffset%32 != 0 {
			t.Fatalf("test encoder write1 at offset %d", writeOffset)
		}
		blob[writeOffset] = value
		writeOffset++
	}
	write31 := func(buf [31]byte) {
		if writeOffset%32 != 1 {
			t.Fatalf("test encoder write31 at offset %d", writeOffset)
		}
		copy(blob[writeOffset:writeOffset+31], buf[:])
		writeOffset += 31
	}
	read1 := func() byte {
		if readOffset >= len(payload) {
			return 0
		}
		value := payload[readOffset]
		readOffset++
		return value
	}
	read31 := func() [31]byte {
		var out [31]byte
		if readOffset >= len(payload) {
			return out
		}
		n := copy(out[:], payload[readOffset:])
		readOffset += n
		return out
	}

	for round := 0; round < testBlobEncodingRounds; round++ {
		if readOffset >= len(payload) {
			break
		}

		var buf31 [31]byte
		if round == 0 {
			length := uint32(len(payload))
			buf31[0] = 0
			buf31[1] = byte(length >> 16)
			buf31[2] = byte(length >> 8)
			buf31[3] = byte(length)
			toCopy := min(len(payload)-readOffset, 27)
			copy(buf31[4:4+toCopy], payload[readOffset:readOffset+toCopy])
			readOffset += toCopy
		} else {
			buf31 = read31()
		}

		x := read1()
		write1(x & 0x3f)
		write31(buf31)

		buf31 = read31()
		y := read1()
		write1((y & 0x0f) | ((x & 0xc0) >> 2))
		write31(buf31)

		buf31 = read31()
		z := read1()
		write1(z & 0x3f)
		write31(buf31)

		buf31 = read31()
		write1(((z & 0xc0) >> 2) | ((y & 0xf0) >> 4))
		write31(buf31)
	}
	if readOffset < len(payload) {
		t.Fatalf("test payload did not fit into one blob")
	}
	return blob
}
