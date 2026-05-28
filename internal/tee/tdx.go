package tee

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const tdxNonceSize = 32

type TDXProvider struct {
	secretDir   string
	socketPath  string
	nonceReader io.Reader
}

type TDXQuote struct {
	document   []byte
	nonce      []byte
	issuerType string
	metadata   json.RawMessage
}

type tdxsRequest struct {
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`
}

type tdxsIssueRequestData struct {
	UserData string `json:"userData"`
	Nonce    string `json:"nonce"`
}

type tdxsIssueResponse struct {
	Data  *tdxsIssueResponseData `json:"data"`
	Error *string                `json:"error"`
}

type tdxsIssueResponseData struct {
	Document string `json:"document"`
}

type tdxsMetadataResponse struct {
	Data  *tdxsMetadataResponseData `json:"data"`
	Error *string                   `json:"error"`
}

type tdxsMetadataResponseData struct {
	IssuerType string          `json:"issuerType"`
	UserData   string          `json:"userData"`
	Nonce      string          `json:"nonce"`
	Metadata   json.RawMessage `json:"metadata"`
}

func NewTDXProvider(cfg Config, nonceReader io.Reader) *TDXProvider {
	socketPath := strings.TrimSpace(cfg.TDXSocket)
	if socketPath == "" {
		socketPath = DefaultTDXSocket
	}
	if nonceReader == nil {
		nonceReader = crand.Reader
	}
	return &TDXProvider{
		secretDir:   cfg.SecretDir,
		socketPath:  socketPath,
		nonceReader: nonceReader,
	}
}

func (p *TDXProvider) LoadQuote(instance common.Address) (Quote, error) {
	var reportData [32]byte
	copy(reportData[:], instance.Bytes())
	quote, err := p.LoadQuoteForReportData(reportData[:])
	if err != nil {
		return nil, err
	}
	tdxQuote, ok := quote.(TDXQuote)
	if !ok {
		return quote, nil
	}
	metadata, err := p.LoadMetadata()
	if err != nil {
		return nil, err
	}
	tdxQuote.issuerType = metadata.IssuerType
	tdxQuote.metadata = metadata.Metadata
	return tdxQuote, nil
}

func (p *TDXProvider) LoadQuoteForReportData(reportData []byte) (Quote, error) {
	nonce := make([]byte, tdxNonceSize)
	if _, err := io.ReadFull(p.nonceReader, nonce); err != nil {
		return nil, fmt.Errorf("generate tdx quote nonce: %w", err)
	}

	requestData, err := json.Marshal(tdxsIssueRequestData{
		UserData: hex.EncodeToString(reportData),
		Nonce:    hex.EncodeToString(nonce),
	})
	if err != nil {
		return nil, err
	}
	responseBytes, err := p.sendTDXSRequest(tdxsRequest{
		Method: "issue",
		Data:   requestData,
	})
	if err != nil {
		return nil, err
	}

	var response tdxsIssueResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return nil, fmt.Errorf("decode tdxs issue response: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("tdxs issue error: %s", *response.Error)
	}
	if response.Data == nil {
		return nil, fmt.Errorf("tdxs issue response missing data")
	}
	document, err := hex.DecodeString(response.Data.Document)
	if err != nil {
		return nil, fmt.Errorf("decode tdxs document: %w", err)
	}

	return TDXQuote{
		document: document,
		nonce:    nonce,
	}, nil
}

func (p *TDXProvider) LoadMetadata() (tdxsMetadataResponseData, error) {
	responseBytes, err := p.sendTDXSRequest(tdxsRequest{
		Method: "metadata",
		Data:   json.RawMessage(`{}`),
	})
	if err != nil {
		return tdxsMetadataResponseData{}, err
	}

	var response tdxsMetadataResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return tdxsMetadataResponseData{}, fmt.Errorf("decode tdxs metadata response: %w", err)
	}
	if response.Error != nil {
		return tdxsMetadataResponseData{}, fmt.Errorf("tdxs metadata error: %s", *response.Error)
	}
	if response.Data == nil {
		return tdxsMetadataResponseData{}, fmt.Errorf("tdxs metadata response missing data")
	}
	return *response.Data, nil
}

func (p *TDXProvider) LoadPrivateKey() (*ecdsa.PrivateKey, error) {
	keyBytes, err := os.ReadFile(filepath.Join(p.secretDir, privateKeyFilename))
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(keyBytes)
}

func (p *TDXProvider) SavePrivateKey(privKey *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(p.secretDir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(p.secretDir, privateKeyFilename)
	return os.WriteFile(keyPath, crypto.FromECDSA(privKey), 0o600)
}

func (p *TDXProvider) sendTDXSRequest(request tdxsRequest) ([]byte, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("unix", p.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect tdxs socket %s: %w", p.socketPath, err)
	}
	defer conn.Close()

	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write tdxs request: %w", err)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		_ = unixConn.CloseWrite()
	}

	response, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read tdxs response: %w", err)
	}
	return response, nil
}

func (q TDXQuote) Bytes() []byte {
	return append([]byte(nil), q.document...)
}

func (q TDXQuote) Nonce() []byte {
	return append([]byte(nil), q.nonce...)
}

func (q TDXQuote) IssuerType() string {
	return q.issuerType
}

func (q TDXQuote) Metadata() json.RawMessage {
	return append(json.RawMessage(nil), q.metadata...)
}
