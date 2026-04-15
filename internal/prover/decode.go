package prover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/types"
)

type rawBlockEnvelope struct {
	Header json.RawMessage `json:"header"`
	Body   rawBlockBody    `json:"body"`
}

type rawBlockBody struct {
	Transactions []json.RawMessage `json:"transactions"`
	Ommers       []json.RawMessage `json:"ommers"`
	Withdrawals  []json.RawMessage `json:"withdrawals"`
}

type rawWitness struct {
	State        []hexutil.Bytes   `json:"state"`
	StateIndices []uint32          `json:"state_indices"`
	Codes        []hexutil.Bytes   `json:"codes"`
	Headers      []json.RawMessage `json:"headers"`
}

type rawWitnessHeader struct {
	Header json.RawMessage `json:"header"`
	Hash   json.RawMessage `json:"hash"`
}

type rawTransactionEnvelope struct {
	Signature   rawTransactionSignature    `json:"signature"`
	Transaction map[string]json.RawMessage `json:"transaction"`
}

type rawTransactionSignature struct {
	R       json.RawMessage `json:"r"`
	S       json.RawMessage `json:"s"`
	YParity json.RawMessage `json:"yParity"`
	V       json.RawMessage `json:"v"`
}

type rawAccessTuple struct {
	Address           string   `json:"address"`
	StorageKeys       []string `json:"storageKeys"`
	StorageKeysLegacy []string `json:"storage_keys"`
}

type rawWithdrawal struct {
	Index                json.RawMessage `json:"index"`
	ValidatorIndex       json.RawMessage `json:"validatorIndex"`
	ValidatorIndexLegacy json.RawMessage `json:"validator_index"`
	Address              json.RawMessage `json:"address"`
	Amount               json.RawMessage `json:"amount"`
}

type quantityValue uint64

func (q *quantityValue) UnmarshalJSON(input []byte) error {
	value, err := parseUint64JSON(input)
	if err != nil {
		return err
	}
	*q = quantityValue(value)
	return nil
}

func decodeBlockEnvelope(raw json.RawMessage) (rawBlockEnvelope, error) {
	var decoded rawBlockEnvelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return rawBlockEnvelope{}, fmt.Errorf("unmarshal block: %w", err)
	}
	return decoded, nil
}

func decodeHeader(raw json.RawMessage) (*types.Header, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal header: %w", err)
	}

	number, err := requireUint64(fields, "number")
	if err != nil {
		return nil, err
	}
	gasLimit, err := requireUint64(fields, "gas_limit", "gasLimit")
	if err != nil {
		return nil, err
	}
	gasUsed, err := requireUint64(fields, "gas_used", "gasUsed")
	if err != nil {
		return nil, err
	}
	timestamp, err := requireUint64(fields, "timestamp")
	if err != nil {
		return nil, err
	}
	difficulty, err := requireBigInt(fields, "difficulty")
	if err != nil {
		return nil, err
	}
	logsBloom, err := requireBytes(fields, "logs_bloom", "logsBloom")
	if err != nil {
		return nil, err
	}
	extraData, err := requireBytes(fields, "extra_data", "extraData")
	if err != nil {
		return nil, err
	}
	parentHash, err := requireHash(fields, "parent_hash", "parentHash")
	if err != nil {
		return nil, err
	}
	ommersHash, err := requireHash(fields, "ommers_hash", "sha3Uncles", "uncleHash")
	if err != nil {
		return nil, err
	}
	stateRoot, err := requireHash(fields, "state_root", "stateRoot")
	if err != nil {
		return nil, err
	}
	txRoot, err := requireHash(fields, "transactions_root", "transactionsRoot")
	if err != nil {
		return nil, err
	}
	receiptRoot, err := requireHash(fields, "receipts_root", "receiptsRoot")
	if err != nil {
		return nil, err
	}
	beneficiary, err := requireAddress(fields, "beneficiary", "miner")
	if err != nil {
		return nil, err
	}
	mixHash, err := requireHash(fields, "mix_hash", "mixHash")
	if err != nil {
		return nil, err
	}
	nonceBytes, err := requireBytes(fields, "nonce")
	if err != nil {
		return nil, err
	}
	if len(nonceBytes) != 8 {
		return nil, fmt.Errorf("field nonce must be 8 bytes, got %d", len(nonceBytes))
	}
	var nonce types.BlockNonce
	copy(nonce[:], nonceBytes)

	header := &types.Header{
		ParentHash:  parentHash,
		UncleHash:   ommersHash,
		Coinbase:    beneficiary,
		Root:        stateRoot,
		TxHash:      txRoot,
		ReceiptHash: receiptRoot,
		Bloom:       types.BytesToBloom(logsBloom),
		Difficulty:  difficulty,
		Number:      new(big.Int).SetUint64(number),
		GasLimit:    gasLimit,
		GasUsed:     gasUsed,
		Time:        timestamp,
		Extra:       extraData,
		MixDigest:   mixHash,
		Nonce:       nonce,
	}

	if baseFee, err := optionalBigInt(fields, "base_fee_per_gas", "baseFeePerGas"); err != nil {
		return nil, err
	} else {
		header.BaseFee = baseFee
	}
	if withdrawalsRoot, err := optionalHash(fields, "withdrawals_root", "withdrawalsRoot"); err != nil {
		return nil, err
	} else {
		header.WithdrawalsHash = withdrawalsRoot
	}
	if blobGasUsed, err := optionalUint64Ptr(fields, "blob_gas_used", "blobGasUsed"); err != nil {
		return nil, err
	} else {
		header.BlobGasUsed = blobGasUsed
	}
	if excessBlobGas, err := optionalUint64Ptr(fields, "excess_blob_gas", "excessBlobGas"); err != nil {
		return nil, err
	} else {
		header.ExcessBlobGas = excessBlobGas
	}
	if parentBeaconRoot, err := optionalHash(fields, "parent_beacon_block_root", "parentBeaconBlockRoot"); err != nil {
		return nil, err
	} else {
		header.ParentBeaconRoot = parentBeaconRoot
	}
	if requestsHash, err := optionalHash(fields, "requests_hash", "requestsHash"); err != nil {
		return nil, err
	} else {
		header.RequestsHash = requestsHash
	}

	return header, nil
}

func decodeTransactions(raws []json.RawMessage) (types.Transactions, error) {
	txs := make(types.Transactions, len(raws))
	for i, rawTx := range raws {
		tx, err := decodeTransaction(rawTx)
		if err != nil {
			return nil, fmt.Errorf("decode transaction %d: %w", i, err)
		}
		txs[i] = tx
	}
	return txs, nil
}

func decodeTransaction(raw json.RawMessage) (*types.Transaction, error) {
	var envelope rawTransactionEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal transaction envelope: %w", err)
	}

	if encoded, ok := envelope.Transaction["Eip1559"]; ok {
		return decodeDynamicFeeTransaction(encoded, envelope.Signature)
	}
	if encoded, ok := envelope.Transaction["Legacy"]; ok {
		return decodeLegacyTransaction(encoded, envelope.Signature)
	}
	return nil, fmt.Errorf("unsupported transaction variants: %v", keysOf(envelope.Transaction))
}

func decodeDynamicFeeTransaction(
	raw json.RawMessage,
	signature rawTransactionSignature,
) (*types.Transaction, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Eip1559 transaction: %w", err)
	}
	nonce, err := requireUint64(fields, "nonce")
	if err != nil {
		return nil, err
	}
	gasLimit, err := requireUint64(fields, "gas_limit", "gas")
	if err != nil {
		return nil, err
	}
	chainID, err := requireBigInt(fields, "chain_id", "chainId")
	if err != nil {
		return nil, err
	}
	gasFeeCap, err := requireBigInt(fields, "max_fee_per_gas", "maxFeePerGas")
	if err != nil {
		return nil, err
	}
	gasTipCap, err := requireBigInt(fields, "max_priority_fee_per_gas", "maxPriorityFeePerGas")
	if err != nil {
		return nil, err
	}
	value, err := optionalBigIntWithDefault(fields, "value")
	if err != nil {
		return nil, err
	}
	input, err := requireBytes(fields, "input", "data")
	if err != nil {
		return nil, err
	}
	accessList, err := optionalAccessList(fields, "access_list", "accessList")
	if err != nil {
		return nil, err
	}
	to, err := optionalAddress(fields, "to")
	if err != nil {
		return nil, err
	}
	v, r, s, err := decodeSignature(signature)
	if err != nil {
		return nil, err
	}

	return types.NewTx(&types.DynamicFeeTx{
		ChainID:    chainID,
		Nonce:      nonce,
		GasTipCap:  gasTipCap,
		GasFeeCap:  gasFeeCap,
		Gas:        gasLimit,
		To:         to,
		Value:      value,
		Data:       input,
		AccessList: accessList,
		V:          v,
		R:          r,
		S:          s,
	}), nil
}

func decodeLegacyTransaction(
	raw json.RawMessage,
	signature rawTransactionSignature,
) (*types.Transaction, error) {
	fields, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal Legacy transaction: %w", err)
	}
	nonce, err := requireUint64(fields, "nonce")
	if err != nil {
		return nil, err
	}
	gasLimit, err := requireUint64(fields, "gas_limit", "gas")
	if err != nil {
		return nil, err
	}
	gasPrice, err := requireBigInt(fields, "gas_price", "gasPrice")
	if err != nil {
		return nil, err
	}
	value, err := optionalBigIntWithDefault(fields, "value")
	if err != nil {
		return nil, err
	}
	input, err := requireBytes(fields, "input", "data")
	if err != nil {
		return nil, err
	}
	to, err := optionalAddress(fields, "to")
	if err != nil {
		return nil, err
	}
	chainID, err := optionalBigInt(fields, "chain_id", "chainId")
	if err != nil {
		return nil, err
	}
	parity, r, s, err := decodeSignature(signature)
	if err != nil {
		return nil, err
	}
	v := legacyV(chainID, parity)

	return types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       to,
		Value:    value,
		Data:     input,
		V:        v,
		R:        r,
		S:        s,
	}), nil
}

func decodeWithdrawals(raws []json.RawMessage) (types.Withdrawals, error) {
	if raws == nil {
		return nil, nil
	}
	if len(raws) == 0 {
		return types.Withdrawals{}, nil
	}
	withdrawals := make(types.Withdrawals, len(raws))
	for i, raw := range raws {
		var decoded rawWithdrawal
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("unmarshal withdrawal %d: %w", i, err)
		}
		index, err := parseUint64JSON(decoded.Index)
		if err != nil {
			return nil, fmt.Errorf("parse withdrawal %d index: %w", i, err)
		}
		validatorIndexRaw := decoded.ValidatorIndex
		if len(validatorIndexRaw) == 0 {
			validatorIndexRaw = decoded.ValidatorIndexLegacy
		}
		validatorIndex, err := parseUint64JSON(validatorIndexRaw)
		if err != nil {
			return nil, fmt.Errorf("parse withdrawal %d validator index: %w", i, err)
		}
		address, err := parseAddressJSON(decoded.Address)
		if err != nil {
			return nil, fmt.Errorf("parse withdrawal %d address: %w", i, err)
		}
		amount, err := parseUint64JSON(decoded.Amount)
		if err != nil {
			return nil, fmt.Errorf("parse withdrawal %d amount: %w", i, err)
		}
		withdrawals[i] = &types.Withdrawal{
			Index:     index,
			Validator: validatorIndex,
			Address:   address,
			Amount:    amount,
		}
	}
	return withdrawals, nil
}

func decodeWitness(raw json.RawMessage) (*ReplayWitness, error) {
	var decoded rawWitness
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal witness: %w", err)
	}
	if len(decoded.StateIndices) > 0 {
		return nil, fmt.Errorf("witness contains state_indices; adapter must expand shared state nodes first")
	}

	fullHeaders, compactAncestors, err := decodeWitnessHeaders(decoded.Headers)
	if err != nil {
		return nil, err
	}
	witness := &stateless.Witness{
		Headers: fullHeaders,
		Codes:   make(map[string]struct{}, len(decoded.Codes)),
		State:   make(map[string]struct{}, len(decoded.State)),
	}
	for _, code := range decoded.Codes {
		witness.Codes[string(code)] = struct{}{}
	}
	for _, node := range decoded.State {
		witness.State[string(node)] = struct{}{}
	}
	return &ReplayWitness{
		Witness:          witness,
		CompactAncestors: compactAncestors,
	}, nil
}

func decodeWitnessHeaders(raws []json.RawMessage) ([]*types.Header, []CompactAncestor, error) {
	if len(raws) == 0 {
		return nil, nil, fmt.Errorf("witness must include at least one ancestor header")
	}
	fullHeaders := make([]*types.Header, 0, 1)
	compactAncestors := make([]CompactAncestor, 0, len(raws))
	for index, raw := range raws {
		var decoded rawWitnessHeader
		if err := json.Unmarshal(raw, &decoded); err == nil && len(decoded.Header) > 0 && !bytes.Equal(decoded.Header, []byte("null")) {
			header, err := decodeHeader(decoded.Header)
			if err != nil {
				return nil, nil, fmt.Errorf("decode witness header %d: %w", index, err)
			}
			fullHeaders = append(fullHeaders, header)
			continue
		}

		fields, err := decodeJSONObject(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decode witness header %d: %w", index, err)
		}
		number, err := requireUint64(fields, "number")
		if err != nil {
			return nil, nil, fmt.Errorf("decode witness header %d number: %w", index, err)
		}
		hash, err := requireHash(fields, "hash")
		if err != nil {
			return nil, nil, fmt.Errorf("decode witness header %d hash: %w", index, err)
		}
		parentHash, err := requireHash(fields, "parent_hash", "parentHash")
		if err != nil {
			return nil, nil, fmt.Errorf("decode witness header %d parent hash: %w", index, err)
		}
		timestamp, err := requireUint64(fields, "timestamp")
		if err != nil {
			return nil, nil, fmt.Errorf("decode witness header %d timestamp: %w", index, err)
		}
		compactAncestors = append(compactAncestors, CompactAncestor{
			Number:     number,
			Hash:       hash,
			ParentHash: parentHash,
			Timestamp:  timestamp,
		})
	}
	if len(fullHeaders) == 0 {
		return nil, nil, fmt.Errorf("witness must include a full parent header")
	}
	return fullHeaders, compactAncestors, nil
}

func decodeJSONObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func requireUint64(fields map[string]json.RawMessage, names ...string) (uint64, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return 0, fmt.Errorf("missing required field %q", names[0])
	}
	value, err := parseUint64JSON(raw)
	if err != nil {
		return 0, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func optionalUint64Ptr(fields map[string]json.RawMessage, names ...string) (*uint64, error) {
	raw, ok := lookupField(fields, names...)
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	value, err := parseUint64JSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return &value, nil
}

func requireBigInt(fields map[string]json.RawMessage, names ...string) (*big.Int, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", names[0])
	}
	value, err := parseBigIntJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func optionalBigInt(fields map[string]json.RawMessage, names ...string) (*big.Int, error) {
	raw, ok := lookupField(fields, names...)
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	value, err := parseBigIntJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func optionalBigIntWithDefault(fields map[string]json.RawMessage, names ...string) (*big.Int, error) {
	value, err := optionalBigInt(fields, names...)
	if value == nil || err != nil {
		if err != nil {
			return nil, err
		}
		return new(big.Int), nil
	}
	return value, nil
}

func requireBytes(fields map[string]json.RawMessage, names ...string) ([]byte, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return nil, fmt.Errorf("missing required field %q", names[0])
	}
	value, err := parseBytesJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func requireHash(fields map[string]json.RawMessage, names ...string) (common.Hash, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return common.Hash{}, fmt.Errorf("missing required field %q", names[0])
	}
	value, err := parseHashJSON(raw)
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func optionalHash(fields map[string]json.RawMessage, names ...string) (*common.Hash, error) {
	raw, ok := lookupField(fields, names...)
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	value, err := parseHashJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return &value, nil
}

func requireAddress(fields map[string]json.RawMessage, names ...string) (common.Address, error) {
	raw, ok := lookupField(fields, names...)
	if !ok {
		return common.Address{}, fmt.Errorf("missing required field %q", names[0])
	}
	value, err := parseAddressJSON(raw)
	if err != nil {
		return common.Address{}, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return value, nil
}

func optionalAddress(fields map[string]json.RawMessage, names ...string) (*common.Address, error) {
	raw, ok := lookupField(fields, names...)
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	value, err := parseAddressJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	return &value, nil
}

func optionalAccessList(
	fields map[string]json.RawMessage,
	names ...string,
) (types.AccessList, error) {
	raw, ok := lookupField(fields, names...)
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var decoded []rawAccessTuple
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("parse field %q: %w", names[0], err)
	}
	accessList := make(types.AccessList, len(decoded))
	for index, item := range decoded {
		keys := item.StorageKeys
		if len(keys) == 0 {
			keys = item.StorageKeysLegacy
		}
		storageKeys := make([]common.Hash, len(keys))
		for keyIndex, key := range keys {
			hash, err := parseHashString(key)
			if err != nil {
				return nil, fmt.Errorf(
					"parse field %q[%d].storageKeys[%d]: %w",
					names[0],
					index,
					keyIndex,
					err,
				)
			}
			storageKeys[keyIndex] = hash
		}
		address, err := parseAddressString(item.Address)
		if err != nil {
			return nil, fmt.Errorf("parse field %q[%d].address: %w", names[0], index, err)
		}
		accessList[index] = types.AccessTuple{
			Address:     address,
			StorageKeys: storageKeys,
		}
	}
	return accessList, nil
}

func decodeSignature(signature rawTransactionSignature) (*big.Int, *big.Int, *big.Int, error) {
	r, err := parseBigIntJSON(signature.R)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse signature.r: %w", err)
	}
	s, err := parseBigIntJSON(signature.S)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse signature.s: %w", err)
	}
	parity := signature.YParity
	if len(parity) == 0 {
		parity = signature.V
	}
	v, err := parseBigIntJSON(parity)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse signature parity: %w", err)
	}
	return v, r, s, nil
}

func legacyV(chainID, parity *big.Int) *big.Int {
	if parity == nil {
		return nil
	}
	if chainID == nil || chainID.Sign() == 0 {
		return new(big.Int).Add(parity, big.NewInt(27))
	}
	v := new(big.Int).Mul(chainID, big.NewInt(2))
	v.Add(v, big.NewInt(35))
	v.Add(v, parity)
	return v
}

func lookupField(fields map[string]json.RawMessage, names ...string) (json.RawMessage, bool) {
	for _, name := range names {
		if raw, ok := fields[name]; ok {
			return raw, true
		}
	}
	return nil, false
}

func parseUint64JSON(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, fmt.Errorf("empty quantity")
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
		return parseUint64String(value)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, err
	}
	return strconv.ParseUint(number.String(), 10, 64)
}

func parseBigIntJSON(raw json.RawMessage) (*big.Int, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("empty quantity")
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return parseBigIntString(value)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return nil, err
	}
	value, ok := new(big.Int).SetString(number.String(), 10)
	if !ok {
		return nil, fmt.Errorf("invalid decimal quantity %q", number.String())
	}
	return value, nil
}

func parseBytesJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("empty bytes value")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return hexutil.Decode(value)
}

func parseHashJSON(raw json.RawMessage) (common.Hash, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return common.Hash{}, fmt.Errorf("empty hash")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return common.Hash{}, err
	}
	return parseHashString(value)
}

func parseAddressJSON(raw json.RawMessage) (common.Address, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return common.Address{}, fmt.Errorf("empty address")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return common.Address{}, err
	}
	return parseAddressString(value)
}

func parseUint64String(value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty quantity")
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		return strconv.ParseUint(value[2:], 16, 64)
	}
	return strconv.ParseUint(value, 10, 64)
}

func parseBigIntString(value string) (*big.Int, error) {
	if value == "" {
		return nil, fmt.Errorf("empty quantity")
	}
	base := 10
	digits := value
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		digits = value[2:]
	}
	parsed, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return nil, fmt.Errorf("invalid quantity %q", value)
	}
	return parsed, nil
}

func parseHashString(value string) (common.Hash, error) {
	decoded, err := hexutil.Decode(value)
	if err != nil {
		return common.Hash{}, err
	}
	if len(decoded) != common.HashLength {
		return common.Hash{}, fmt.Errorf("expected %d bytes, got %d", common.HashLength, len(decoded))
	}
	return common.BytesToHash(decoded), nil
}

func parseAddressString(value string) (common.Address, error) {
	decoded, err := hexutil.Decode(value)
	if err != nil {
		return common.Address{}, err
	}
	if len(decoded) != common.AddressLength {
		return common.Address{}, fmt.Errorf("expected %d bytes, got %d", common.AddressLength, len(decoded))
	}
	return common.BytesToAddress(decoded), nil
}

func keysOf(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
