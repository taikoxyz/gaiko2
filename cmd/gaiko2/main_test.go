package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
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
	serveFn = func(net.Listener, *http.Server) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run([]string{"server", ":18080"}, &stdout)
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
	serveFn = func(net.Listener, *http.Server) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run([]string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "listening on 127.0.0.1:18080") {
		t.Fatalf("expected startup log, got %q", stdout.String())
	}
}

func TestRunServerDoesNotLimitProvingDurationAtHTTPServer(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
	})

	listenFn = func(network, addr string) (net.Listener, error) {
		return fakeListener{addr: fakeAddr("127.0.0.1:18080")}, nil
	}
	serveFn = func(_ net.Listener, server *http.Server) error {
		if server.ReadHeaderTimeout != 10*time.Second {
			t.Fatalf("unexpected read header timeout: %s", server.ReadHeaderTimeout)
		}
		if server.ReadTimeout != 0 || server.WriteTimeout != 0 || server.IdleTimeout != 0 {
			t.Fatalf(
				"unexpected body/proving timeouts: read=%s write=%s idle=%s",
				server.ReadTimeout,
				server.WriteTimeout,
				server.IdleTimeout,
			)
		}
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run([]string{"server", ":18080"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
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
	serveFn = func(net.Listener, *http.Server) error {
		return errors.New("stop server")
	}

	var stdout bytes.Buffer
	err := run([]string{"server"}, &stdout)
	if err == nil || err.Error() != "stop server" {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "listening on 127.0.0.1:19090") {
		t.Fatalf("expected startup log, got %q", stdout.String())
	}
}

func TestRunServerTEERequiresAPIKey(t *testing.T) {
	prevListen := listenFn
	prevServe := serveFn
	t.Cleanup(func() {
		listenFn = prevListen
		serveFn = prevServe
	})

	setEnv(t, "GAIKO2_PROVING_MODE", "tee")
	t.Cleanup(func() {
		_ = os.Unsetenv("GAIKO2_API_KEY")
	})
	_ = os.Unsetenv("GAIKO2_API_KEY")

	listenFn = func(string, string) (net.Listener, error) {
		t.Fatalf("server must fail before listening")
		return nil, nil
	}
	serveFn = func(net.Listener, *http.Server) error {
		t.Fatalf("server must fail before serving")
		return nil
	}

	var stdout bytes.Buffer
	err := run([]string{"server"}, &stdout)
	if err == nil || err.Error() != "GAIKO2_API_KEY is required in tee mode" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIServerConfigLeavesBodyLimitDisabledByDefault(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Unsetenv("GAIKO2_API_KEY")
		_ = os.Unsetenv("GAIKO2_MAX_BODY_BYTES")
	})
	_ = os.Unsetenv("GAIKO2_API_KEY")
	_ = os.Unsetenv("GAIKO2_MAX_BODY_BYTES")

	cfg, err := apiServerConfigFromEnv("native")
	if err != nil {
		t.Fatalf("api config: %v", err)
	}
	if cfg.MaxBodyBytes != 0 {
		t.Fatalf("expected default body limit disabled, got %d", cfg.MaxBodyBytes)
	}
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
	if err := run([]string{"bootstrap"}, &stdout); err != nil {
		t.Fatalf("run bootstrap: %v", err)
	}
	if !called {
		t.Fatalf("expected bootstrap command to be invoked")
	}
	if stdout.String() != "bootstrapped\n" {
		t.Fatalf("unexpected bootstrap output: %q", stdout.String())
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
	if err := run([]string{"check"}, &stdout); err != nil {
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
	if err := run([]string{"metadata"}, &stdout); err != nil {
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
