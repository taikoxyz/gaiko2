package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/taikoxyz/gaiko2/internal/protocol"
	"github.com/taikoxyz/gaiko2/internal/prover"
)

const (
	proveShastaPath          = "/prove/shasta"
	proveShastaAggregatePath = "/prove/shasta-aggregate"
)

func NewServer(service prover.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(proveShastaPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected POST")
			return
		}

		var req protocol.ShastaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}

		validated, err := prover.ValidateRequest(req)
		if err != nil {
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
			writeError(w, statusCode, code, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	mux.HandleFunc(proveShastaAggregatePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "expected POST")
			return
		}

		var req protocol.ShastaAggregateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}

		validated, err := prover.ValidateAggregateRequest(req)
		if err != nil {
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
			writeError(w, statusCode, code, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	return mux
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
