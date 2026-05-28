package tee

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewProviderAcceptsTDXType(t *testing.T) {
	provider, err := NewProvider(Config{
		Type:      TypeTDX,
		SecretDir: t.TempDir(),
		TDXSocket: filepath.Join(t.TempDir(), "tdxs.sock"),
	})
	if err != nil {
		t.Fatalf("new tdx provider: %v", err)
	}
	if _, ok := provider.(*TDXProvider); !ok {
		t.Fatalf("expected *TDXProvider, got %T", provider)
	}
}

func TestTDXProviderLoadQuoteForReportDataUsesTDXSSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "tdxs.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})

	wantReportData := []byte{0xaa, 0xbb, 0xcc}
	wantNonce := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}
	wantDocument := []byte{0xde, 0xad, 0xbe, 0xef}
	requestCh := make(chan tdxsRequest, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var req tdxsRequest
		if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
			t.Errorf("decode tdxs request: %v", err)
			return
		}
		requestCh <- req
		_ = json.NewEncoder(conn).Encode(tdxsIssueResponse{
			Data: &tdxsIssueResponseData{Document: hex.EncodeToString(wantDocument)},
		})
	}()

	provider := NewTDXProvider(Config{
		SecretDir: t.TempDir(),
		TDXSocket: socketPath,
	}, fixedNonceReader{nonce: wantNonce})
	quote, err := provider.LoadQuoteForReportData(wantReportData)
	if err != nil {
		t.Fatalf("load tdx quote: %v", err)
	}
	if got := quote.Bytes(); hex.EncodeToString(got) != hex.EncodeToString(wantDocument) {
		t.Fatalf("quote mismatch: got %x want %x", got, wantDocument)
	}
	tdxQuote, ok := quote.(TDXQuote)
	if !ok {
		t.Fatalf("expected TDXQuote, got %T", quote)
	}
	if got := tdxQuote.Nonce(); hex.EncodeToString(got) != hex.EncodeToString(wantNonce) {
		t.Fatalf("nonce mismatch: got %x want %x", got, wantNonce)
	}

	req := <-requestCh
	if req.Method != "issue" {
		t.Fatalf("unexpected method: %s", req.Method)
	}
	var data tdxsIssueRequestData
	if err := json.Unmarshal(req.Data, &data); err != nil {
		t.Fatalf("unmarshal issue data: %v", err)
	}
	if data.UserData != hex.EncodeToString(wantReportData) {
		t.Fatalf("userData mismatch: got %s want %x", data.UserData, wantReportData)
	}
	if data.Nonce != hex.EncodeToString(wantNonce) {
		t.Fatalf("nonce mismatch: got %s want %x", data.Nonce, wantNonce)
	}
}

func TestTDXProviderLoadQuoteIncludesTDXSMetadataForBootstrap(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "tdxs.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})

	wantDocument := []byte{0xca, 0xfe}
	wantMetadata := json.RawMessage(`{"mrTd":"0xabc"}`)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req tdxsRequest
			if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
				t.Errorf("decode tdxs request: %v", err)
				_ = conn.Close()
				return
			}
			switch req.Method {
			case "issue":
				_ = json.NewEncoder(conn).Encode(tdxsIssueResponse{
					Data: &tdxsIssueResponseData{Document: hex.EncodeToString(wantDocument)},
				})
			case "metadata":
				_ = json.NewEncoder(conn).Encode(tdxsMetadataResponse{
					Data: &tdxsMetadataResponseData{
						IssuerType: "tdx",
						Metadata:   wantMetadata,
					},
				})
			default:
				t.Errorf("unexpected method: %s", req.Method)
			}
			_ = conn.Close()
		}
	}()

	provider := NewTDXProvider(Config{
		SecretDir: t.TempDir(),
		TDXSocket: socketPath,
	}, fixedNonceReader{nonce: bytesOf(0x42, tdxNonceSize)})
	quote, err := provider.LoadQuote(common.HexToAddress("0x0000777735367b36bc9b61c50022d9d0700db4ec"))
	if err != nil {
		t.Fatalf("load bootstrap quote: %v", err)
	}
	tdxQuote, ok := quote.(TDXQuote)
	if !ok {
		t.Fatalf("expected TDXQuote, got %T", quote)
	}
	if tdxQuote.IssuerType() != "tdx" {
		t.Fatalf("unexpected issuer type: %s", tdxQuote.IssuerType())
	}
	if string(tdxQuote.Metadata()) != string(wantMetadata) {
		t.Fatalf("metadata mismatch: got %s want %s", tdxQuote.Metadata(), wantMetadata)
	}
}

type fixedNonceReader struct {
	nonce []byte
}

func (r fixedNonceReader) Read(out []byte) (int, error) {
	copy(out, r.nonce)
	return len(out), nil
}

func bytesOf(value byte, length int) []byte {
	out := make([]byte, length)
	for i := range out {
		out[i] = value
	}
	return out
}
