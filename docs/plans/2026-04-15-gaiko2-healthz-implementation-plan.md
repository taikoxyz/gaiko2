# gaiko2 Healthz Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a minimal `GET /healthz` endpoint to `gaiko2` for liveness probing.

**Architecture:** Extend the existing HTTP mux in `internal/api/server.go` with a lightweight handler that returns a small JSON body and does not depend on prover state. Cover the route with focused API tests and update README usage notes.

**Tech Stack:** Go, `net/http`, Go test

---

### Task 1: Add API Tests

**Files:**
- Modify: `internal/api/server_test.go`
- Test: `internal/api/server_test.go`

**Step 1: Write the failing tests**

Add:

```go
func TestNewServerReturnsHealthzOK(t *testing.T) {}

func TestNewServerRejectsNonGetHealthz(t *testing.T) {}
```

Expect:
- `GET /healthz` returns `200` and `{"status":"ok"}`
- `POST /healthz` returns `405`

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run 'TestNewServer(ReturnsHealthzOK|RejectsNonGetHealthz)'`
Expected: FAIL because the route does not exist yet.

**Step 3: Write minimal implementation**

Add a health handler in `internal/api/server.go` that:
- matches `/healthz`
- accepts only `GET`
- returns `{"status":"ok"}`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run 'TestNewServer(ReturnsHealthzOK|RejectsNonGetHealthz)'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -m "feat: add healthz endpoint"
```

### Task 2: Document the Endpoint

**Files:**
- Modify: `README.md`

**Step 1: Add the endpoint to README**

Document that the server exposes:
- `GET /healthz`
- `POST /prove/shasta`
- `POST /prove/shasta-aggregate`

**Step 2: Run a quick repo-wide verification**

Run: `go test ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: mention healthz endpoint"
```
