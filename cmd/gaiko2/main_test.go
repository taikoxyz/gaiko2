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
)

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
	serveFn = func(net.Listener, http.Handler) error {
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
	serveFn = func(net.Listener, http.Handler) error {
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
