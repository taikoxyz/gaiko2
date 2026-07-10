package prover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

func TestDecodeTransactionEip2930(t *testing.T) {
	chainID := big.NewInt(167001)
	key, err := crypto.HexToECDSA(manifestTestTxPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	to := common.HexToAddress(testAddress("41"))
	accessAddress := common.HexToAddress(testAddress("42"))
	storageKey := common.HexToHash(testHash("43"))
	transaction, err := types.SignTx(types.NewTx(&types.AccessListTx{
		ChainID:  chainID,
		Nonce:    7,
		GasPrice: big.NewInt(11),
		Gas:      50_000,
		To:       &to,
		Value:    big.NewInt(13),
		Data:     []byte{0x12, 0x34},
		AccessList: types.AccessList{{
			Address:     accessAddress,
			StorageKeys: []common.Hash{storageKey},
		}},
	}), types.NewEIP2930Signer(chainID), key)
	if err != nil {
		t.Fatalf("sign EIP-2930 transaction: %v", err)
	}
	v, r, s := transaction.RawSignatureValues()
	raw := json.RawMessage(fmt.Sprintf(`{
		"signature": {"r": "0x%s", "s": "0x%s", "yParity": "0x%s"},
		"transaction": {"Eip2930": {
			"chain_id": "0x%x", "nonce": "0x7", "gas_price": "0xb",
			"gas_limit": "0xc350", "to": %q, "value": "0xd", "input": "0x1234",
			"access_list": [{"address": %q, "storage_keys": [%q]}]
		}}
	}`, r.Text(16), s.Text(16), v.Text(16), chainID, to.Hex(), accessAddress.Hex(), storageKey.Hex()))

	decoded, err := decodeTransaction(raw)
	if err != nil {
		t.Fatalf("decode EIP-2930 transaction: %v", err)
	}
	assertTransactionEncodingEqual(t, decoded, transaction)
}

func TestDecodeTransactionEip7702(t *testing.T) {
	chainID := big.NewInt(167001)
	key, err := crypto.HexToECDSA(manifestTestTxPrivateKeyHex)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	to := common.HexToAddress(testAddress("51"))
	delegation := common.HexToAddress(testAddress("52"))
	authorization, err := types.SignSetCode(key, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegation,
		Nonce:   3,
	})
	if err != nil {
		t.Fatalf("sign EIP-7702 authorization: %v", err)
	}
	transaction, err := types.SignTx(types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     9,
		GasTipCap: uint256.NewInt(2),
		GasFeeCap: uint256.NewInt(20),
		Gas:       100_000,
		To:        to,
		Value:     uint256.NewInt(17),
		Data:      []byte{0xab, 0xcd},
		AccessList: types.AccessList{{
			Address: delegation,
		}},
		AuthList: []types.SetCodeAuthorization{authorization},
	}), types.NewPragueSigner(chainID), key)
	if err != nil {
		t.Fatalf("sign EIP-7702 transaction: %v", err)
	}
	v, r, s := transaction.RawSignatureValues()
	raw := json.RawMessage(fmt.Sprintf(`{
		"signature": {"r": "0x%s", "s": "0x%s", "yParity": "0x%s"},
		"transaction": {"Eip7702": {
			"chain_id": "0x%x", "nonce": "0x9", "max_priority_fee_per_gas": "0x2",
			"max_fee_per_gas": "0x14", "gas_limit": "0x186a0", "to": %q,
			"value": "0x11", "input": "0xabcd",
			"access_list": [{"address": %q, "storage_keys": []}],
			"authorization_list": [{
				"chain_id": "0x%s", "address": %q, "nonce": "0x3",
				"yParity": "0x%x", "r": "0x%s", "s": "0x%s"
			}]
		}}
	}`,
		r.Text(16), s.Text(16), v.Text(16), chainID, to.Hex(), delegation.Hex(),
		authorization.ChainID.ToBig().Text(16), authorization.Address.Hex(), authorization.V,
		authorization.R.ToBig().Text(16), authorization.S.ToBig().Text(16),
	))

	decoded, err := decodeTransaction(raw)
	if err != nil {
		t.Fatalf("decode EIP-7702 transaction: %v", err)
	}
	assertTransactionEncodingEqual(t, decoded, transaction)
}

func assertTransactionEncodingEqual(t *testing.T, got *types.Transaction, want *types.Transaction) {
	t.Helper()
	gotBytes, err := got.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal decoded transaction: %v", err)
	}
	wantBytes, err := want.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal expected transaction: %v", err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("transaction encoding mismatch:\n got %x\nwant %x", gotBytes, wantBytes)
	}
}
