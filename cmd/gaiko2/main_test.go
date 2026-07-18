package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/taikoxyz/gaiko2/internal/prover"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

func TestRunServerPrintsStartupSummary(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
	})

	setEnv(t, "GAIKO2_PROVING_MODE", "native")
	setEnv(t, "GAIKO2_FORK", "shasta")
	setEnv(t, "GAIKO2_INSTANCE_ID", "11")
	setEnv(t, "GAIKO2_CONFIG_DIR", "/var/lib/gaiko2/config")
	setEnv(t, "GAIKO2_SECRET_DIR", "/var/lib/gaiko2/secrets")

	listenFn = func(network, addr string) (net.Listener, error) {
		return fakeListener{addr: fakeAddr("127.0.0.1:18080")}, nil
	}
	serveFn = func(context.Context, net.Listener, http.Handler) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "starting gaiko2 provider") {
		t.Fatalf("expected startup summary, got %q", output)
	}
	if !strings.Contains(output, "mode=native") ||
		!strings.Contains(output, "fork=shasta") ||
		!strings.Contains(output, "instance_id=11") {
		t.Fatalf("expected startup fields, got %q", output)
	}
}

func TestRunServerPrintsListeningAddress(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
	})

	listenFn = func(network, addr string) (net.Listener, error) {
		return fakeListener{addr: fakeAddr("127.0.0.1:18080")}, nil
	}
	serveFn = func(context.Context, net.Listener, http.Handler) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "listening on 127.0.0.1:18080") {
		t.Fatalf("expected startup log, got %q", stdout.String())
	}
}

func TestFormatListeningAddrNormalizesWildcardTCP(t *testing.T) {
	addr := formatListeningAddr(&net.TCPAddr{Port: 18080})
	if addr != "0.0.0.0:18080" {
		t.Fatalf("unexpected formatted addr: %s", addr)
	}
}

func TestRunServerUsesPortFromEnv(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
	})

	if err := os.Setenv("GAIKO2_PORT", "19090"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("GAIKO2_PORT")
	})

	listenFn = func(network, addr string) (net.Listener, error) {
		if addr != ":19090" {
			t.Fatalf("unexpected listen addr: %s", addr)
		}
		return fakeListener{addr: fakeAddr("127.0.0.1:19090")}, nil
	}
	serveFn = func(context.Context, net.Listener, http.Handler) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "listening on 127.0.0.1:19090") {
		t.Fatalf("expected startup log, got %q", stdout.String())
	}
}

func TestRunServerRejectsV1WithoutGuestInput(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	prevNewReplayService := newReplayServiceFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
		newReplayServiceFn = prevNewReplayService
	})

	setEnv(t, "GAIKO2_INSTANCE_ID", "11")

	listenFn = func(network, addr string) (net.Listener, error) {
		return fakeListener{addr: fakeAddr("127.0.0.1:18080")}, nil
	}
	newReplayServiceFn = func(cfg prover.ServiceConfig, runner prover.Runner) (prover.Service, error) {
		return prover.StubService{}, nil
	}
	serveFn = func(_ context.Context, _ net.Listener, handler http.Handler) error {
		req := httptest.NewRequest(http.MethodPost, "/prove/shasta", bytes.NewBufferString(`{
			"schema":"raiko2-shasta-request-v1",
			"payload":{}
		}`))
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "guest_input") {
			t.Fatalf("expected missing guest_input response, got %q", body)
		}
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewHTTPServerSetsConnectionTimeouts(t *testing.T) {
	server := newHTTPServer(http.NewServeMux())
	if server.ReadHeaderTimeout <= 0 {
		t.Fatalf("expected positive ReadHeaderTimeout, got %v", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout <= 0 {
		t.Fatalf("expected positive IdleTimeout, got %v", server.IdleTimeout)
	}
}

func TestRunServerShutsDownGracefullyOnContextCancel(t *testing.T) {
	prevListen := listenFn
	t.Cleanup(func() {
		listenFn = prevListen
	})

	addrCh := make(chan net.Addr, 1)
	listenFn = func(network, _ string) (net.Listener, error) {
		listener, err := net.Listen(network, "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		addrCh <- listener.Addr()
		return listener, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	var stdout bytes.Buffer
	go func() {
		done <- run(ctx, []string{"server", ":0"}, &stdout)
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never started listening")
	}
	waitForHealthz(t, addr)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after context cancel")
	}
}

func waitForHealthz(t *testing.T, addr net.Addr) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr.String() + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server never became healthy")
}

func TestRunBootstrapDispatchesLifecycleCommand(t *testing.T) {
	prev := bootstrapCommandFn
	t.Cleanup(func() {
		bootstrapCommandFn = prev
	})

	called := false
	bootstrapCommandFn = func(args []string, stdout io.Writer) error {
		called = true
		if len(args) != 0 {
			t.Fatalf("unexpected bootstrap args: %v", args)
		}
		_, err := io.WriteString(stdout, "bootstrapped\n")
		return err
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"bootstrap"}, &stdout); err != nil {
		t.Fatalf("run bootstrap: %v", err)
	}
	if !called {
		t.Fatalf("expected bootstrap command to be invoked")
	}
	if stdout.String() != "bootstrapped\n" {
		t.Fatalf("unexpected bootstrap output: %q", stdout.String())
	}
}

func TestRunBootstrapCommandExplainsForceOnExistingKey(t *testing.T) {
	prev := teeBootstrapFn
	t.Cleanup(func() {
		teeBootstrapFn = prev
	})

	teeBootstrapFn = func(tee.Provider, bool) (tee.BootstrapData, error) {
		return tee.BootstrapData{}, tee.ErrPrivateKeyExists
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"bootstrap", "--secret-dir", t.TempDir(), "--config-dir", t.TempDir(),
	}, &stdout)
	if !errors.Is(err, tee.ErrPrivateKeyExists) {
		t.Fatalf("expected ErrPrivateKeyExists, got %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected guidance mentioning --force, got %q", err.Error())
	}
}

func TestRunBootstrapCommandForwardsForceFlag(t *testing.T) {
	prev := teeBootstrapFn
	t.Cleanup(func() {
		teeBootstrapFn = prev
	})

	var gotForce bool
	teeBootstrapFn = func(_ tee.Provider, force bool) (tee.BootstrapData, error) {
		gotForce = force
		return tee.BootstrapData{}, nil
	}

	var stdout bytes.Buffer
	err := run(context.Background(), []string{
		"bootstrap", "--force", "--secret-dir", t.TempDir(), "--config-dir", t.TempDir(),
	}, &stdout)
	if err != nil {
		t.Fatalf("run bootstrap --force: %v", err)
	}
	if !gotForce {
		t.Fatalf("expected --force to be forwarded to tee.Bootstrap")
	}
}

func TestRunCheckDispatchesLifecycleCommand(t *testing.T) {
	prev := checkCommandFn
	t.Cleanup(func() {
		checkCommandFn = prev
	})

	called := false
	checkCommandFn = func(args []string, stdout io.Writer) error {
		called = true
		if len(args) != 0 {
			t.Fatalf("unexpected check args: %v", args)
		}
		_, err := io.WriteString(stdout, "checked\n")
		return err
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"check"}, &stdout); err != nil {
		t.Fatalf("run check: %v", err)
	}
	if !called {
		t.Fatalf("expected check command to be invoked")
	}
	if stdout.String() != "checked\n" {
		t.Fatalf("unexpected check output: %q", stdout.String())
	}
}

func TestRunMetadataDispatchesLifecycleCommand(t *testing.T) {
	prev := metadataCommandFn
	t.Cleanup(func() {
		metadataCommandFn = prev
	})

	called := false
	metadataCommandFn = func(args []string, stdout io.Writer) error {
		called = true
		if len(args) != 0 {
			t.Fatalf("unexpected metadata args: %v", args)
		}
		_, err := io.WriteString(stdout, "metadata\n")
		return err
	}

	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"metadata"}, &stdout); err != nil {
		t.Fatalf("run metadata: %v", err)
	}
	if !called {
		t.Fatalf("expected metadata command to be invoked")
	}
	if stdout.String() != "metadata\n" {
		t.Fatalf("unexpected metadata output: %q", stdout.String())
	}
}

type fakeListener struct {
	addr net.Addr
}

func (f fakeListener) Accept() (net.Conn, error) { return nil, errors.New("unused") }
func (f fakeListener) Close() error              { return nil }
func (f fakeListener) Addr() net.Addr            { return f.addr }

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env %s: %v", key, err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv(key)
	})
}
