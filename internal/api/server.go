package api

import (
	"encoding/json"
	"errors"
	"fmt"
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
			metadata := proveShastaRequestLogMetadata(req, err)
			log.Printf(
				"failed prove/shasta request schema=%s chain_id=%d block_count=%d code=%s message=%q",
				metadata.Schema,
				metadata.ChainID,
				metadata.BlockCount,
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
				validated.Request.Schema,
				validated.Request.Payload.ChainID,
				len(validated.Request.Payload.Blocks),
				code,
				err.Error(),
			)
			writeError(w, statusCode, code, err.Error())
			return
		}

		log.Printf(
			"completed prove/shasta request schema=%s proposal_id=%d chain_id=%d block_count=%d",
			validated.Request.Schema,
			validated.Carry.TransitionInput.ProposalID,
			validated.Request.Payload.ChainID,
			len(validated.Request.Payload.Blocks),
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
			"completed prove/shasta-aggregate request schema=%s proposal_ids=%s proof_count=%d",
			req.Schema,
			aggregateProposalIDSummary(validated.Proofs),
			len(req.Payload.Proofs),
		)
		writeJSON(w, http.StatusOK, protocol.Success(result))
	})
	return mux
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

func proveShastaRequestLogMetadata(req protocol.ShastaRequest, err error) prover.RequestLogMetadata {
	metadata := prover.RequestLogMetadata{
		Schema:     req.Schema,
		ChainID:    req.Payload.ChainID,
		BlockCount: len(req.Payload.Blocks),
	}
	var validationErr *prover.ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Metadata
	}
	return metadata
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
