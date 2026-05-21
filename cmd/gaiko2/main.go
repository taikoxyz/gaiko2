package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/taikoxyz/gaiko2/internal/api"
	"github.com/taikoxyz/gaiko2/internal/prover"
	"github.com/taikoxyz/gaiko2/internal/tee"
)

var (
	listenFn           = net.Listen
	serveFn            = serveHTTP
	bootstrapCommandFn = runBootstrapCommand
	checkCommandFn     = runCheckCommand
	metadataCommandFn  = runMetadataCommand
)

const (
	envPort            = "GAIKO2_PORT"
	envAttestationPath = "GAIKO2_ATTESTATION_PATH"
	envAPIKey          = "GAIKO2_API_KEY"
	envMaxBodyBytes    = "GAIKO2_MAX_BODY_BYTES"
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
		mode := normalizedProvingMode(cfg.Mode)
		apiCfg, err := apiServerConfigFromEnv(mode)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(
			stdout,
			"starting gaiko2 provider mode=%s tee_type=%s fork=%s instance_id=%d config_dir=%s secret_dir=%s listen=%s\n",
			mode,
			strings.TrimSpace(cfg.TeeType),
			strings.TrimSpace(cfg.Fork),
			cfg.InstanceID,
			cfg.ConfigDir,
			cfg.SecretDir,
			addr,
		)
		service, err := prover.NewConfiguredReplayService(cfg, nil)
		if err != nil {
			return err
		}
		listener, err := listenFn("tcp", addr)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "listening on %s\n", formatListeningAddr(listener.Addr()))
		return serveFn(listener, &http.Server{
			Handler: api.NewServerWithConfig(service, apiCfg),
		})
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serveHTTP(listener net.Listener, server *http.Server) error {
	return server.Serve(listener)
}

func apiServerConfigFromEnv(mode string) (api.ServerConfig, error) {
	apiKey := strings.TrimSpace(os.Getenv(envAPIKey))
	if mode == prover.ProvingModeTEE && apiKey == "" {
		return api.ServerConfig{}, fmt.Errorf("%s is required in tee mode", envAPIKey)
	}

	maxBodyBytes := int64(0)
	if value := strings.TrimSpace(os.Getenv(envMaxBodyBytes)); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return api.ServerConfig{}, fmt.Errorf("parse %s: expected positive integer", envMaxBodyBytes)
		}
		maxBodyBytes = parsed
	}

	return api.ServerConfig{
		APIKey:       apiKey,
		MaxBodyBytes: maxBodyBytes,
	}, nil
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

	data, err := tee.Bootstrap(provider)
	if err != nil {
		return err
	}
	if err := tee.SaveBootstrapData(*configDir, data); err != nil {
		return err
	}
	attestationPath := strings.TrimSpace(os.Getenv(envAttestationPath))
	if attestationPath != "" {
		metadata, err := tee.ReadAttestationMetadataFile(attestationPath)
		if err != nil {
			return err
		}
		if err := tee.SaveAttestationMetadata(*configDir, metadata); err != nil {
			return err
		}
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
