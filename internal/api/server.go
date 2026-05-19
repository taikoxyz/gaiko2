package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/prover"
)

const (
	healthzPath              = "/healthz"
	proveShastaPath          = "/prove/shasta"
	proveShastaAggregatePath = "/prove/shasta-aggregate"
)

func NewServer(service prover.Service) http.Handler {
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

		var req protocol.ShastaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
			"completed prove/shasta request schema=%s chain_id=%d block_count=%d input_prefix=%q",
			req.Schema,
			req.Payload.ChainID,
			len(req.Payload.Blocks),
			shortInputPrefix(result.Input),
		)
		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	mux.HandleFunc(proveShastaAggregatePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected POST")
			return
		}

		var req protocol.ShastaAggregateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
			"completed prove/shasta-aggregate request schema=%s proof_count=%d input_prefix=%q",
			req.Schema,
			len(req.Payload.Proofs),
			shortInputPrefix(result.Input),
		)
		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	return mux
}

func shortInputPrefix(input string) string {
	const prefixLen = 12
	if len(input) <= prefixLen {
		return input
	}
	return input[:prefixLen] + "..."
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
