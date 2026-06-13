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

type L2HeaderSource interface {
	HeaderByNumber(context.Context, uint64) (L2Header, error)
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
	requestBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []any{quantity(number), false},
		ID:      1,
	})
	if err != nil {
		return L2Header{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return L2Header{}, err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return L2Header{}, fmt.Errorf("local L2 eth_getBlockByNumber(%d): %w", number, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return L2Header{}, fmt.Errorf("local L2 eth_getBlockByNumber(%d) returned HTTP %d", number, resp.StatusCode)
	}

	var rpcResp blockByNumberResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return L2Header{}, fmt.Errorf("decode local L2 response: %w", err)
	}
	if rpcResp.Error != nil {
		return L2Header{}, fmt.Errorf("local L2 RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		return L2Header{}, fmt.Errorf("local L2 missing block %d", number)
	}
	return rpcResp.Result.header()
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
