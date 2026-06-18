package tee

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
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

type fixedNonceReader struct {
	nonce []byte
}

func (r fixedNonceReader) Read(out []byte) (int, error) {
	copy(out, r.nonce)
	return len(out), nil
}
