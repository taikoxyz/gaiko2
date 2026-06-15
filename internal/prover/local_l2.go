package prover

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const DefaultLocalL2RPCURL = "http://127.0.0.1:8545"
const shastaExtraDataLen = 7

type L2Header struct {
	Number          uint64
	Hash            common.Hash
	ParentHash      common.Hash
	StateRoot       common.Hash
	ReceiptsRoot    common.Hash
	ProposalID      uint64
	ProposalIDValid bool
}

type L1Origin struct {
	BlockID            uint64
	L2BlockHash        common.Hash
	L1BlockHeight      uint64
	L1BlockHeightValid bool
	L1BlockHash        common.Hash
	L1BlockHashValid   bool
}

type L2HeaderSource interface {
	HeaderByNumber(context.Context, uint64) (L2Header, error)
	LastCertainBlockIDByBatchID(context.Context, uint64) (uint64, error)
	LastCertainL1OriginByBatchID(context.Context, uint64) (L1Origin, error)
}

type LocalL2RPC struct {
	endpoint string
	client   *http.Client
}

type L2RPCOptions struct {
	AllowRemote bool
}

var newLocalL2HeaderSourceFn = func(rawURL string, opts L2RPCOptions) (L2HeaderSource, error) {
	return NewLocalL2RPCWithOptions(rawURL, opts)
}

func NewLocalL2RPC(rawURL string) (*LocalL2RPC, error) {
	return NewLocalL2RPCWithOptions(rawURL, L2RPCOptions{})
}

func NewLocalL2RPCWithOptions(rawURL string, opts L2RPCOptions) (*LocalL2RPC, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		rawURL = DefaultLocalL2RPCURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse local L2 RPC URL: %w", err)
	}
	if !opts.AllowRemote && !isLocalHTTPURL(parsed) {
		return nil, fmt.Errorf("tdxgeth L2 RPC URL must be local, got %q", rawURL)
	}
	return &LocalL2RPC{
		endpoint: rawURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}, nil
}

func (c *LocalL2RPC) HeaderByNumber(ctx context.Context, number uint64) (L2Header, error) {
	var rpcResp blockByNumberResponse
	if err := c.call(ctx, "eth_getBlockByNumber", []any{quantity(number), false}, &rpcResp); err != nil {
		return L2Header{}, fmt.Errorf("local L2 eth_getBlockByNumber(%d): %w", number, err)
	}
	if rpcResp.Result == nil {
		return L2Header{}, fmt.Errorf("local L2 missing block %d", number)
	}
	return rpcResp.Result.header()
}

func (c *LocalL2RPC) LastCertainBlockIDByBatchID(ctx context.Context, batchID uint64) (uint64, error) {
	var rpcResp quantityResponse
	if err := c.call(
		ctx,
		"taikoAuth_lastCertainBlockIDByBatchID",
		[]any{quantity(batchID)},
		&rpcResp,
	); err != nil {
		return 0, fmt.Errorf("local L2 taikoAuth_lastCertainBlockIDByBatchID(%d): %w", batchID, err)
	}
	if rpcResp.Result == nil {
		return 0, fmt.Errorf("local L2 missing certain last block for proposal %d", batchID)
	}
	blockID, err := parseQuantity(*rpcResp.Result)
	if err != nil {
		return 0, fmt.Errorf("parse certain last block for proposal %d: %w", batchID, err)
	}
	return blockID, nil
}

func (c *LocalL2RPC) LastCertainL1OriginByBatchID(ctx context.Context, batchID uint64) (L1Origin, error) {
	var rpcResp l1OriginResponse
	if err := c.call(
		ctx,
		"taikoAuth_lastCertainL1OriginByBatchID",
		[]any{quantity(batchID)},
		&rpcResp,
	); err != nil {
		return L1Origin{}, fmt.Errorf("local L2 taikoAuth_lastCertainL1OriginByBatchID(%d): %w", batchID, err)
	}
	if rpcResp.Result == nil {
		return L1Origin{}, fmt.Errorf("local L2 missing certain L1 origin for proposal %d", batchID)
	}
	origin, err := rpcResp.Result.origin()
	if err != nil {
		return L1Origin{}, fmt.Errorf("parse certain L1 origin for proposal %d: %w", batchID, err)
	}
	return origin, nil
}

func (c *LocalL2RPC) call(ctx context.Context, method string, params []any, out any) error {
	requestBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s returned HTTP %d", method, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode local L2 response: %w", err)
	}
	return rpcError(out)
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type blockByNumberResponse struct {
	Result *rawL2Header  `json:"result"`
	Error  *jsonRPCError `json:"error"`
}

type quantityResponse struct {
	Result *string       `json:"result"`
	Error  *jsonRPCError `json:"error"`
}

type l1OriginResponse struct {
	Result *rawL1Origin  `json:"result"`
	Error  *jsonRPCError `json:"error"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rawL2Header struct {
	Number       string `json:"number"`
	Hash         string `json:"hash"`
	ParentHash   string `json:"parentHash"`
	StateRoot    string `json:"stateRoot"`
	ReceiptsRoot string `json:"receiptsRoot"`
	ExtraData    string `json:"extraData"`
}

type rawL1Origin struct {
	BlockID       string  `json:"blockID"`
	L2BlockHash   string  `json:"l2BlockHash"`
	L1BlockHeight *string `json:"l1BlockHeight"`
	L1BlockHash   *string `json:"l1BlockHash"`
}

func (h rawL2Header) header() (L2Header, error) {
	number, err := parseQuantity(h.Number)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 block number: %w", err)
	}
	hash, err := parseHashString(h.Hash)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 block hash: %w", err)
	}
	parentHash, err := parseHashString(h.ParentHash)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 parent hash: %w", err)
	}
	stateRoot, err := parseHashString(h.StateRoot)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 state root: %w", err)
	}
	receiptsRoot, err := parseHashString(h.ReceiptsRoot)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 receipts root: %w", err)
	}
	proposalID, proposalIDValid, err := decodeShastaProposalIDFromExtraData(h.ExtraData)
	if err != nil {
		return L2Header{}, fmt.Errorf("parse local L2 extraData: %w", err)
	}
	return L2Header{
		Number:          number,
		Hash:            hash,
		ParentHash:      parentHash,
		StateRoot:       stateRoot,
		ReceiptsRoot:    receiptsRoot,
		ProposalID:      proposalID,
		ProposalIDValid: proposalIDValid,
	}, nil
}

func (o rawL1Origin) origin() (L1Origin, error) {
	blockID, err := parseQuantity(o.BlockID)
	if err != nil {
		return L1Origin{}, fmt.Errorf("parse blockID: %w", err)
	}
	l2BlockHash, err := parseHashString(o.L2BlockHash)
	if err != nil {
		return L1Origin{}, fmt.Errorf("parse l2BlockHash: %w", err)
	}
	origin := L1Origin{
		BlockID:     blockID,
		L2BlockHash: l2BlockHash,
	}
	if o.L1BlockHeight != nil {
		height, err := parseQuantity(*o.L1BlockHeight)
		if err != nil {
			return L1Origin{}, fmt.Errorf("parse l1BlockHeight: %w", err)
		}
		origin.L1BlockHeight = height
		origin.L1BlockHeightValid = true
	}
	if o.L1BlockHash != nil {
		hash, err := parseHashString(*o.L1BlockHash)
		if err != nil {
			return L1Origin{}, fmt.Errorf("parse l1BlockHash: %w", err)
		}
		origin.L1BlockHash = hash
		origin.L1BlockHashValid = true
	}
	return origin, nil
}

func rpcError(out any) error {
	switch resp := out.(type) {
	case *blockByNumberResponse:
		if resp.Error != nil {
			return fmt.Errorf("local L2 RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
	case *quantityResponse:
		if resp.Error != nil {
			return fmt.Errorf("local L2 RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
	case *l1OriginResponse:
		if resp.Error != nil {
			return fmt.Errorf("local L2 RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
	}
	return nil
}

func decodeShastaProposalIDFromExtraData(raw string) (uint64, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, false, nil
	}
	decoded, err := parseHexBytes(raw)
	if err != nil {
		return 0, false, err
	}
	if len(decoded) < shastaExtraDataLen {
		return 0, false, nil
	}

	var proposalBytes [8]byte
	copy(proposalBytes[2:], decoded[1:shastaExtraDataLen])
	return binary.BigEndian.Uint64(proposalBytes[:]), true, nil
}

func isLocalHTTPURL(parsed *url.URL) bool {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func quantity(value uint64) string {
	return "0x" + strconv.FormatUint(value, 16)
}

func parseQuantity(raw string) (uint64, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 16, 64)
}
