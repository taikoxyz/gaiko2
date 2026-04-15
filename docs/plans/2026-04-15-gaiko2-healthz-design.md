# gaiko2 Healthz Design

## Goal

Add a minimal health check endpoint to `gaiko2` so operators and containers can verify the HTTP server is alive without invoking prover logic.

## Scope

- Add `GET /healthz`
- Return `200 OK` with a tiny JSON body: `{"status":"ok"}`
- Reject non-`GET` methods with the existing JSON error envelope and `405`

Out of scope:

- No readiness checks
- No signer, key, quote, or prover dependency checks
- No Docker `HEALTHCHECK` instruction changes

## Rationale

`gaiko2` already exposes only proving endpoints. A minimal `healthz` endpoint gives operators a stable probe target for local development, Docker, and compose without creating ambiguity about prover readiness.

Keeping the endpoint liveness-only avoids mixing two different concerns:

- liveness: the HTTP process is serving requests
- readiness: the prover and signer stack are fully initialized

If readiness becomes necessary later, it should be introduced as a separate endpoint with explicit semantics.

## API

### `GET /healthz`

Response:

```json
{
  "status": "ok"
}
```

### Invalid method

`POST /healthz` and any other non-`GET` method return the standard JSON error envelope with `405 METHOD_NOT_ALLOWED`.

## Implementation Notes

- Add a new route in `internal/api/server.go`
- Keep the response shape local to the API layer; it does not need to reuse proof response types
- Add unit tests for success and method rejection in `internal/api/server_test.go`
- Update README server usage notes to mention the endpoint

## Verification

- `go test ./...`
- Optional smoke:
  - run `gaiko2 server :8080`
  - `curl http://127.0.0.1:8080/healthz`
