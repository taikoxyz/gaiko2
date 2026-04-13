package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/taikoxyz/gaiko2/internal/api"
	"github.com/taikoxyz/gaiko2/internal/prover"
)

var (
	listenFn = net.Listen
	serveFn  = http.Serve
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, "gaiko2 dev")
		return err
	case "server", "serve", "s":
		addr := ":8080"
		if len(args) > 1 {
			addr = args[1]
		}
		cfg, err := prover.ServiceConfigFromEnv()
		if err != nil {
			return err
		}
		service, err := prover.NewConfiguredReplayService(cfg, nil)
		if err != nil {
			return err
		}
		listener, err := listenFn("tcp", addr)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "listening on %s\n", formatListeningAddr(listener.Addr()))
		return serveFn(listener, api.NewServer(service))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func formatListeningAddr(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return addr.String()
	}

	if tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return net.JoinHostPort("0.0.0.0", strconv.Itoa(tcpAddr.Port))
	}
	return tcpAddr.String()
}

func printUsage(stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout, "gaiko2")
	_, _ = fmt.Fprintln(stdout, "")
	_, _ = fmt.Fprintln(stdout, "Usage:")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 help")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 version")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 server")
}
