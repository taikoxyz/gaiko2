package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/taikoxyz/gaiko2/internal/api"
	"github.com/taikoxyz/gaiko2/internal/prover"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

var (
	listenFn           = net.Listen
	serveFn            = serveHTTP
	newReplayServiceFn = func(cfg prover.ServiceConfig, runner prover.Runner) (prover.Service, error) {
		return prover.NewConfiguredReplayService(cfg, runner)
	}
	bootstrapCommandFn               = runBootstrapCommand
	checkCommandFn                   = runCheckCommand
	metadataCommandFn                = runMetadataCommand
	teeBootstrapFn                   = tee.Bootstrap
	teeBootstrapDataForExistingKeyFn = tee.BootstrapDataForExistingKey
	bootstrapStderr                  = io.Writer(os.Stderr)
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverIdleTimeout       = 2 * time.Minute
	serverShutdownGrace     = 30 * time.Second
)

func newHTTPServer(handler http.Handler) *http.Server {
	// ReadTimeout/WriteTimeout stay unset: request bodies reach ~92MB and
	// proving runs for minutes, so only header reads and idle keep-alives
	// are bounded here.
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

func normalizeServeError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serveHTTP(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := newHTTPServer(handler)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		return normalizeServeError(err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
		return normalizeServeError(<-serveErr)
	}
}

const (
	envPort            = "GAIKO2_PORT"
	envAttestationPath = "GAIKO2_ATTESTATION_PATH"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, "gaiko2 dev")
		return err
	case "bootstrap":
		return bootstrapCommandFn(args[1:], stdout)
	case "check":
		return checkCommandFn(args[1:], stdout)
	case "metadata":
		return metadataCommandFn(args[1:], stdout)
	case "server", "serve", "s":
		addr := defaultServerAddr()
		if len(args) > 1 {
			addr = args[1]
		}
		cfg, err := prover.ServiceConfigFromEnv()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(
			stdout,
			"starting gaiko2 provider mode=%s tee_type=%s fork=%s instance_id=%d config_dir=%s secret_dir=%s listen=%s\n",
			normalizedProvingMode(cfg.Mode),
			strings.TrimSpace(cfg.TeeType),
			strings.TrimSpace(cfg.Fork),
			cfg.InstanceID,
			cfg.ConfigDir,
			cfg.SecretDir,
			addr,
		)
		service, err := newReplayServiceFn(cfg, nil)
		if err != nil {
			return err
		}
		listener, err := listenFn("tcp", addr)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "listening on %s\n", formatListeningAddr(listener.Addr()))
		return serveFn(ctx, listener, api.NewServer(service))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func normalizedProvingMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return prover.ProvingModeNative
	}
	return mode
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

func defaultServerAddr() string {
	port := strings.TrimSpace(os.Getenv(envPort))
	if port == "" {
		return ":8080"
	}
	if strings.HasPrefix(port, ":") {
		return port
	}
	return ":" + port
}

func printUsage(stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout, "gaiko2")
	_, _ = fmt.Fprintln(stdout, "")
	_, _ = fmt.Fprintln(stdout, "Usage:")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 help")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 version")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 bootstrap")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 check")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 metadata")
	_, _ = fmt.Fprintln(stdout, "  gaiko2 server")
}

func runBootstrapCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	teeType := flags.String("tee-type", strings.TrimSpace(os.Getenv("GAIKO2_TEE_TYPE")), "tee provider type")
	secretDir := flags.String("secret-dir", envOrDefault("GAIKO2_SECRET_DIR", tee.DefaultSecretDir()), "directory for sealed keys")
	configDir := flags.String("config-dir", envOrDefault("GAIKO2_CONFIG_DIR", tee.DefaultConfigDir()), "directory for bootstrap metadata")
	force := flags.Bool("force", false, "regenerate the tee key even if a sealed key already exists")
	if err := flags.Parse(args); err != nil {
		return err
	}

	provider, err := tee.NewProvider(tee.Config{
		Type:      *teeType,
		SecretDir: *secretDir,
	})
	if err != nil {
		return err
	}

	if *force {
		if _, err := fmt.Fprintln(
			bootstrapStderr,
			"WARNING: --force replaces any existing tee key; the old key and any on-chain registration bound to it become unusable",
		); err != nil {
			return fmt.Errorf("write bootstrap force warning: %w", err)
		}
	}

	data, err := teeBootstrapFn(provider, *force)
	saveBootstrapData := true
	var existingKeyErr error
	if errors.Is(err, tee.ErrPrivateKeyExists) {
		recovered, matches, recoverErr := teeBootstrapDataForExistingKeyFn(provider, *configDir)
		if recoverErr != nil {
			if errors.Is(recoverErr, tee.ErrPrivateKeyUnavailable) {
				return fmt.Errorf(
					"%w; recover bootstrap data: %w; re-run with --force to replace it (the old key and any on-chain registration bound to it become unusable)",
					err,
					recoverErr,
				)
			}
			return fmt.Errorf("recover bootstrap data: %w", recoverErr)
		}
		data = recovered
		if matches {
			saveBootstrapData = false
			existingKeyErr = fmt.Errorf(
				"%w; re-run with --force to replace it (the old key and any on-chain registration bound to it become unusable)",
				err,
			)
		}
	} else if err != nil {
		return err
	}
	if saveBootstrapData {
		if err := tee.SaveBootstrapData(*configDir, data); err != nil {
			return err
		}
	}
	attestationPath := strings.TrimSpace(os.Getenv(envAttestationPath))
	if attestationPath != "" {
		metadata, err := tee.ReadAttestationMetadataFile(attestationPath)
		if err != nil {
			return errors.Join(existingKeyErr, fmt.Errorf("read attestation metadata: %w", err))
		}
		if err := tee.SaveAttestationMetadata(*configDir, metadata); err != nil {
			return errors.Join(existingKeyErr, fmt.Errorf("save attestation metadata: %w", err))
		}
	}
	if existingKeyErr != nil {
		return existingKeyErr
	}
	return json.NewEncoder(stdout).Encode(data)
}

func runCheckCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	teeType := flags.String("tee-type", strings.TrimSpace(os.Getenv("GAIKO2_TEE_TYPE")), "tee provider type")
	secretDir := flags.String("secret-dir", envOrDefault("GAIKO2_SECRET_DIR", tee.DefaultSecretDir()), "directory for sealed keys")
	if err := flags.Parse(args); err != nil {
		return err
	}

	provider, err := tee.NewProvider(tee.Config{
		Type:      *teeType,
		SecretDir: *secretDir,
	})
	if err != nil {
		return err
	}
	if err := tee.Check(provider); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "ok")
	return err
}

func runMetadataCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("metadata", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	path := flags.String("path", strings.TrimSpace(os.Getenv(envAttestationPath)), "path to image attestation metadata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*path) == "" {
		return fmt.Errorf("%s is not set", envAttestationPath)
	}

	metadata, err := tee.ReadAttestationMetadataFile(*path)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(metadata)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
