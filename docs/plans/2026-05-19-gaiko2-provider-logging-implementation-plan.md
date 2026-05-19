# Gaiko2 Provider Logging Implementation Plan

## Goal

Add minimal startup and request success/failure logs to `gaiko2-sgxgeth` without changing proof behavior.

## Tasks

1. Add failing tests for startup summary output and API request logging.
2. Add lightweight startup summary output in `cmd/gaiko2/main.go`.
3. Add lightweight success/failure logs in `internal/api/server.go`.
4. Run focused Go tests plus formatting checks.
