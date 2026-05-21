package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/prover"
)

const (
	healthzPath              = "/healthz"
	proveShastaPath          = "/prove/shasta"
	proveShastaAggregatePath = "/prove/shasta-aggregate"
)

func NewServer(service prover.Service) http.Handler {
	return NewServerWithConfig(service, ServerConfig{})
}

type ServerConfig struct {
	APIKey       string
	MaxBodyBytes int64
}

func NewServerWithConfig(service prover.Service, cfg ServerConfig) http.Handler {
	cfg = normalizeServerConfig(cfg)
	mux := http.NewServeMux()
	mux.HandleFunc(healthzPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected GET")
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{
			Status: "ok",
		})
	})
	mux.HandleFunc(proveShastaPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected POST")
			return
		}
		if !authorizeRequest(w, r, cfg.APIKey) {
			return
		}

		var req protocol.ShastaRequest
		if err := decodeRequestJSON(w, r, cfg.MaxBodyBytes, &req); err != nil {
			if errors.Is(err, errRequestTooLarge) {
				log.Printf("failed prove/shasta request code=%s message=%q", "REQUEST_TOO_LARGE", err.Error())
				writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", err.Error())
				return
			}
			log.Printf("failed prove/shasta request code=%s message=%q", "INVALID_JSON", err.Error())
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}

		validated, err := prover.ValidateRequest(req)
		if err != nil {
			log.Printf(
				"failed prove/shasta request schema=%s chain_id=%d block_count=%d code=%s message=%q",
				req.Schema,
				req.Payload.ChainID,
				len(req.Payload.Blocks),
				"INVALID_REQUEST",
				err.Error(),
			)
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}

		result, err := service.Prove(r.Context(), validated)
		if err != nil {
			statusCode := http.StatusInternalServerError
			code := "PROVER_ERROR"
			if errors.Is(err, prover.ErrNotImplemented) {
				statusCode = http.StatusNotImplemented
				code = "NOT_IMPLEMENTED"
			}
			log.Printf(
				"failed prove/shasta request schema=%s chain_id=%d block_count=%d code=%s message=%q",
				req.Schema,
				req.Payload.ChainID,
				len(req.Payload.Blocks),
				code,
				err.Error(),
			)
			writeError(w, statusCode, code, err.Error())
			return
		}

		log.Printf(
			"completed prove/shasta request schema=%s proposal_id=%d chain_id=%d block_count=%d",
			req.Schema,
			validated.Carry.TransitionInput.ProposalID,
			req.Payload.ChainID,
			len(req.Payload.Blocks),
		)
		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	mux.HandleFunc(proveShastaAggregatePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected POST")
			return
		}
		if !authorizeRequest(w, r, cfg.APIKey) {
			return
		}

		var req protocol.ShastaAggregateRequest
		if err := decodeRequestJSON(w, r, cfg.MaxBodyBytes, &req); err != nil {
			if errors.Is(err, errRequestTooLarge) {
				log.Printf("failed prove/shasta-aggregate request code=%s message=%q", "REQUEST_TOO_LARGE", err.Error())
				writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", err.Error())
				return
			}
			log.Printf("failed prove/shasta-aggregate request code=%s message=%q", "INVALID_JSON", err.Error())
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}

		validated, err := prover.ValidateAggregateRequest(req)
		if err != nil {
			log.Printf(
				"failed prove/shasta-aggregate request schema=%s proof_count=%d code=%s message=%q",
				req.Schema,
				len(req.Payload.Proofs),
				"INVALID_REQUEST",
				err.Error(),
			)
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}

		result, err := service.Aggregate(r.Context(), validated)
		if err != nil {
			statusCode := http.StatusInternalServerError
			code := "PROVER_ERROR"
			if errors.Is(err, prover.ErrNotImplemented) {
				statusCode = http.StatusNotImplemented
				code = "NOT_IMPLEMENTED"
			}
			log.Printf(
				"failed prove/shasta-aggregate request schema=%s proof_count=%d code=%s message=%q",
				req.Schema,
				len(req.Payload.Proofs),
				code,
				err.Error(),
			)
			writeError(w, statusCode, code, err.Error())
			return
		}

		log.Printf(
			"completed prove/shasta-aggregate request schema=%s proposal_ids=%s proof_count=%d",
			req.Schema,
			aggregateProposalIDSummary(validated.Proofs),
			len(req.Payload.Proofs),
		)
		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	return mux
}

var errRequestTooLarge = errors.New("request body exceeds configured limit")

func normalizeServerConfig(cfg ServerConfig) ServerConfig {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	return cfg
}

func authorizeRequest(w http.ResponseWriter, r *http.Request, expected string) bool {
	if expected == "" {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(requestAPIKey(r)), []byte(expected)) == 1 {
		return true
	}
	writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid API key")
	return false
}

func requestAPIKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Key")); value != "" {
		return value
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= len("Bearer ") && strings.EqualFold(auth[:len("Bearer ")], "Bearer ") {
		return strings.TrimSpace(auth[len("Bearer "):])
	}
	return ""
}

func decodeRequestJSON(w http.ResponseWriter, r *http.Request, maxBodyBytes int64, value any) error {
	if maxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(value); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return errRequestTooLarge
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain exactly one JSON value")
	}
	return nil
}

func aggregateProposalIDSummary(proofs []prover.AggregateProofView) string {
	if len(proofs) == 0 {
		return "none"
	}
	first := proofs[0].Carry.TransitionInput.ProposalID
	last := proofs[len(proofs)-1].Carry.TransitionInput.ProposalID
	if first == last {
		return fmt.Sprintf("%d", first)
	}
	return fmt.Sprintf("%d..%d", first, last)
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(w, statusCode, protocol.Failure(protocol.ProofError{
		Code:    code,
		Message: message,
	}))
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
