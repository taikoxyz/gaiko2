# Gaiko2 Shasta V1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a lightweight `gaiko2` service that replays `raiko2` Shasta witnesses with `taiko-geth` in TEE and integrate it into `raiko2` through a stable `gaiko2-shasta-v1` execution-packet protocol.

**Architecture:** `raiko2` keeps canonical preflight and validation, then adapts `raiko2_primitives_shasta::GuestInput` into a stable JSON execution packet and submits it to `gaiko2` over internal HTTP. `gaiko2` validates the packet, replays each block with `taiko-geth` stateless execution, checks continuity against `proof_carry_data`, and returns a TEE proof envelope that `raiko2` maps back into canonical `Proof`.

**Tech Stack:** Rust (`raiko2`, `serde`, `reqwest`, `axum`), Go (`gaiko2`, `net/http`, `urfave/cli/v2`, `taiko-geth` stateless execution), JSON schema envelopes, existing TEE signer/quote abstractions from `gaiko`.

---

### Task 1: Define the stable `gaiko2` wire protocol in `raiko2`

**Files:**
- Create: `raiko2/crates/prover/src/gaiko2/protocol.rs`
- Modify: `raiko2/crates/prover/src/lib.rs`
- Test: `raiko2/crates/prover/tests/gaiko2_protocol_roundtrip.rs`

**Step 1: Write the failing test**

```rust
#[test]
fn shasta_packet_roundtrip_preserves_schema_and_payload() {
    let packet = Gaiko2ShastaRequest {
        schema: "gaiko2-shasta-v1".to_string(),
        payload: Gaiko2ShastaPayload {
            chain_id: 167013,
            blocks: vec![Gaiko2ReplayBlock::default()],
            proof_carry_data: ProofCarryData::default(),
        },
    };

    let json = serde_json::to_string(&packet).expect("serialize");
    let decoded: Gaiko2ShastaRequest = serde_json::from_str(&json).expect("deserialize");
    assert_eq!(decoded.schema, "gaiko2-shasta-v1");
    assert_eq!(decoded.payload.chain_id, 167013);
}
```

**Step 2: Run test to verify it fails**

Run: `cargo test -p raiko2-prover gaiko2_protocol_roundtrip -- --nocapture`

Expected: FAIL because `gaiko2::protocol` types do not exist yet.

**Step 3: Write minimal implementation**

```rust
#[derive(Debug, Clone, Serialize, Deserialize, Default, PartialEq)]
pub struct Gaiko2ShastaRequest {
    pub schema: String,
    pub payload: Gaiko2ShastaPayload,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default, PartialEq)]
pub struct Gaiko2ShastaPayload {
    pub chain_id: u64,
    pub blocks: Vec<Gaiko2ReplayBlock>,
    pub proof_carry_data: ProofCarryData,
}
```

**Step 4: Run test to verify it passes**

Run: `cargo test -p raiko2-prover gaiko2_protocol_roundtrip -- --nocapture`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add crates/prover/src/lib.rs crates/prover/src/gaiko2/protocol.rs crates/prover/tests/gaiko2_protocol_roundtrip.rs
git commit -m "feat(prover): define gaiko2 shasta protocol envelope"
```

### Task 2: Add the `raiko2` adapter from `GuestInput` to execution packet

**Files:**
- Create: `raiko2/crates/prover/src/gaiko2/adapter.rs`
- Modify: `raiko2/crates/prover/src/gaiko2/protocol.rs`
- Test: `raiko2/crates/prover/tests/gaiko2_adapter.rs`

**Step 1: Write the failing test**

```rust
#[test]
fn adapter_projects_guest_input_into_execution_packet() {
    let input = fixture_guest_input();
    let packet = build_shasta_packet(&input).expect("build packet");

    assert_eq!(packet.schema, "gaiko2-shasta-v1");
    assert_eq!(packet.payload.blocks.len(), input.witnesses.len());
    assert_eq!(packet.payload.chain_id, input.proof_carry_data.chain_id);
    assert_eq!(
        packet.payload.proof_carry_data.transition_input.checkpoint,
        input.proof_carry_data.transition_input.checkpoint
    );
}
```

**Step 2: Run test to verify it fails**

Run: `cargo test -p raiko2-prover gaiko2_adapter -- --nocapture`

Expected: FAIL because `build_shasta_packet` does not exist.

**Step 3: Write minimal implementation**

```rust
pub fn build_shasta_packet(input: &GuestInput) -> RaikoResult<Gaiko2ShastaRequest> {
    Ok(Gaiko2ShastaRequest {
        schema: "gaiko2-shasta-v1".to_string(),
        payload: Gaiko2ShastaPayload {
            chain_id: input.proof_carry_data.chain_id,
            blocks: input
                .witnesses
                .iter()
                .cloned()
                .map(|w| Gaiko2ReplayBlock {
                    block: w.block,
                    witness: w.witness,
                })
                .collect(),
            proof_carry_data: input.proof_carry_data.clone(),
        },
    })
}
```

**Step 4: Run test to verify it passes**

Run: `cargo test -p raiko2-prover gaiko2_adapter -- --nocapture`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add crates/prover/src/gaiko2/adapter.rs crates/prover/src/gaiko2/protocol.rs crates/prover/tests/gaiko2_adapter.rs
git commit -m "feat(prover): adapt shasta guest input to gaiko2 packet"
```

### Task 3: Scaffold the `gaiko2` repository and protocol package

**Files:**
- Create: `gaiko2/go.mod`
- Create: `gaiko2/README.md`
- Create: `gaiko2/cmd/gaiko2/main.go`
- Create: `gaiko2/internal/protocol/shasta_v1.go`
- Test: `gaiko2/internal/protocol/shasta_v1_test.go`

**Step 1: Write the failing test**

```go
func TestShastaV1RoundTrip(t *testing.T) {
    input := ShastaRequest{
        Schema: "gaiko2-shasta-v1",
        Payload: ShastaPayload{
            ChainID: 167013,
            Blocks: []ReplayBlock{{}},
        },
    }
    data, err := json.Marshal(input)
    require.NoError(t, err)

    var decoded ShastaRequest
    require.NoError(t, json.Unmarshal(data, &decoded))
    require.Equal(t, "gaiko2-shasta-v1", decoded.Schema)
}
```

**Step 2: Run test to verify it fails**

Run: `cd <gaiko2-checkout> && go test ./internal/protocol/...`

Expected: FAIL because the module and protocol package do not exist yet.

**Step 3: Write minimal implementation**

```go
type ShastaRequest struct {
    Schema  string        `json:"schema"`
    Payload ShastaPayload `json:"payload"`
}

type ShastaPayload struct {
    ChainID uint64        `json:"chain_id"`
    Blocks  []ReplayBlock `json:"blocks"`
}
```

**Step 4: Run test to verify it passes**

Run: `cd <gaiko2-checkout> && go test ./internal/protocol/...`

Expected: PASS

**Step 5: Commit**

```bash
cd <gaiko2-checkout>
git init
git add go.mod README.md cmd/gaiko2/main.go internal/protocol/shasta_v1.go internal/protocol/shasta_v1_test.go
git commit -m "feat: scaffold gaiko2 protocol module"
```

### Task 4: Implement `gaiko2` packet validation and geth stateless replay

**Files:**
- Create: `gaiko2/internal/prover/validate.go`
- Create: `gaiko2/internal/prover/replay.go`
- Create: `gaiko2/internal/prover/types.go`
- Test: `gaiko2/internal/prover/replay_test.go`

**Step 1: Write the failing test**

```go
func TestValidateRequestRejectsCheckpointMismatch(t *testing.T) {
    req := fixtureRequest()
    req.Payload.ProofCarryData.TransitionInput.Checkpoint.BlockNumber++

    err := ValidateRequest(req)
    require.Error(t, err)
    require.Contains(t, err.Error(), "checkpoint")
}
```

**Step 2: Run test to verify it fails**

Run: `cd <gaiko2-checkout> && go test ./internal/prover -run TestValidateRequestRejectsCheckpointMismatch -v`

Expected: FAIL because validation logic does not exist.

**Step 3: Write minimal implementation**

```go
func ValidateRequest(req protocol.ShastaRequest) error {
    if req.Schema != protocol.ShastaSchemaV1 {
        return fmt.Errorf("unsupported schema %s", req.Schema)
    }
    if len(req.Payload.Blocks) == 0 {
        return errors.New("empty blocks")
    }
    return nil
}

func ReplayBlocks(ctx context.Context, req protocol.ShastaRequest) (*ReplayResult, error) {
    // Convert packet witness into taiko-geth stateless.Witness, replay each block,
    // and compare state/receipt roots with the block headers.
}
```

**Step 4: Run test to verify it passes**

Run: `cd <gaiko2-checkout> && go test ./internal/prover -run TestValidateRequestRejectsCheckpointMismatch -v`

Expected: PASS

**Step 5: Commit**

```bash
cd <gaiko2-checkout>
git add internal/prover/validate.go internal/prover/replay.go internal/prover/types.go internal/prover/replay_test.go
git commit -m "feat: add gaiko2 packet validation and replay core"
```

### Task 5: Implement `gaiko2` proof generation and HTTP route

**Files:**
- Create: `gaiko2/internal/api/server.go`
- Create: `gaiko2/internal/api/handler.go`
- Create: `gaiko2/internal/prover/prove.go`
- Create: `gaiko2/internal/tee/provider.go`
- Test: `gaiko2/internal/api/handler_test.go`

**Step 1: Write the failing test**

```go
func TestProveHandlerReturnsProofEnvelope(t *testing.T) {
    srv := httptest.NewServer(NewRouter(fakeProver{}))
    defer srv.Close()

    res, err := http.Post(srv.URL+"/internal/prove/shasta-proposal", "application/json", bytes.NewReader(fixtureBody()))
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, res.StatusCode)
}
```

**Step 2: Run test to verify it fails**

Run: `cd <gaiko2-checkout> && go test ./internal/api -run TestProveHandlerReturnsProofEnvelope -v`

Expected: FAIL because the HTTP route does not exist.

**Step 3: Write minimal implementation**

```go
func (h *Handler) ProveShastaProposal(w http.ResponseWriter, r *http.Request) {
    var req protocol.ShastaRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_JSON", err)
        return
    }
    result, err := h.prover.Prove(r.Context(), req)
    if err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
        return
    }
    json.NewEncoder(w).Encode(result)
}
```

**Step 4: Run test to verify it passes**

Run: `cd <gaiko2-checkout> && go test ./internal/api -run TestProveHandlerReturnsProofEnvelope -v`

Expected: PASS

**Step 5: Commit**

```bash
cd <gaiko2-checkout>
git add internal/api/server.go internal/api/handler.go internal/prover/prove.go internal/tee/provider.go internal/api/handler_test.go
git commit -m "feat: expose gaiko2 shasta prove endpoint"
```

### Task 6: Add the `Gaiko2Prover` client to `raiko2`

**Files:**
- Create: `raiko2/crates/prover/src/gaiko2/client.rs`
- Create: `raiko2/crates/prover/src/gaiko2/mod.rs`
- Modify: `raiko2/crates/prover/src/lib.rs`
- Test: `raiko2/crates/prover/tests/gaiko2_client.rs`

**Step 1: Write the failing test**

```rust
#[tokio::test]
async fn gaiko2_client_maps_success_response_into_proof() {
    let server = mock_gaiko2_server(success_response_json());
    let prover = Gaiko2Prover::new(server.url());

    let proof = prover.prove(fixture_guest_input(), &serde_json::json!({}), &NativeBackend).await.expect("proof");
    assert!(proof.proof.is_some());
    assert!(proof.quote.is_some());
    assert!(proof.input.is_some());
}
```

**Step 2: Run test to verify it fails**

Run: `cargo test -p raiko2-prover gaiko2_client -- --nocapture`

Expected: FAIL because `Gaiko2Prover` does not exist.

**Step 3: Write minimal implementation**

```rust
pub struct Gaiko2Prover {
    client: reqwest::Client,
    base_url: String,
}

impl Gaiko2Prover {
    pub fn new(base_url: impl Into<String>) -> Self { /* ... */ }
}
```

**Step 4: Run test to verify it passes**

Run: `cargo test -p raiko2-prover gaiko2_client -- --nocapture`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add crates/prover/src/lib.rs crates/prover/src/gaiko2/mod.rs crates/prover/src/gaiko2/client.rs crates/prover/tests/gaiko2_client.rs
git commit -m "feat(prover): add gaiko2 remote prover client"
```

### Task 7: Wire `proof_type=sgx` to the new `gaiko2` route in `raiko2`

**Files:**
- Modify: `raiko2/bin/raiko2/src/config/prover.rs`
- Modify: `raiko2/bin/raiko2/src/server/state/mod.rs`
- Modify: `raiko2/bin/raiko2/src/server/handlers/proof.rs`
- Modify: `raiko2/config.example.toml`
- Test: `raiko2/bin/raiko2/src/server/e2e.rs`

**Step 1: Write the failing test**

```rust
#[tokio::test]
async fn sgx_requests_route_to_shasta_sgx_remote_pipeline() {
    let route = route_for_proof_type(&state, HoodiProofType::Sgx).expect("route");
    assert_eq!(route.route.to_string(), "sgx/remote");
}
```

**Step 2: Run test to verify it fails**

Run: `cargo test -p raiko2 -- sgx_requests_route_to_shasta_sgx_remote_pipeline --nocapture`

Expected: FAIL because `sgx` is currently rejected.

**Step 3: Write minimal implementation**

```rust
pub enum GuestSystem {
    Risc0,
    Sp1,
    Native,
    Sgx,
}

pub enum RunnerKind {
    Local,
    Boundless,
    Remote,
}
```

**Step 4: Run test to verify it passes**

Run: `cargo test -p raiko2 -- sgx_requests_route_to_shasta_sgx_remote_pipeline --nocapture`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add bin/raiko2/src/config/prover.rs bin/raiko2/src/server/state/mod.rs bin/raiko2/src/server/handlers/proof.rs config.example.toml bin/raiko2/src/server/e2e.rs
git commit -m "feat(server): route sgx proofs to gaiko2"
```

### Task 8: Add end-to-end fixtures and protocol docs

**Files:**
- Create: `raiko2/crates/prover/tests/fixtures/gaiko2_shasta_v1_request.json`
- Create: `raiko2/crates/prover/tests/fixtures/gaiko2_proof_v1_response.json`
- Modify: `raiko2/README.md`
- Modify: `raiko2/docs/API.md`
- Modify: `gaiko2/README.md`

**Step 1: Write the failing test**

```rust
#[test]
fn gaiko2_fixture_request_deserializes() {
    let raw = include_str!("fixtures/gaiko2_shasta_v1_request.json");
    let _: Gaiko2ShastaRequest = serde_json::from_str(raw).expect("fixture parses");
}
```

**Step 2: Run test to verify it fails**

Run: `cargo test -p raiko2-prover gaiko2_fixture_request_deserializes -- --nocapture`

Expected: FAIL because fixtures do not exist yet.

**Step 3: Write minimal implementation**

```json
{
  "schema": "gaiko2-shasta-v1",
  "payload": {
    "chain_id": 167013,
    "blocks": [],
    "proof_carry_data": {}
  }
}
```

**Step 4: Run test to verify it passes**

Run: `cargo test -p raiko2-prover gaiko2_fixture_request_deserializes -- --nocapture`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add crates/prover/tests/fixtures/gaiko2_shasta_v1_request.json crates/prover/tests/fixtures/gaiko2_proof_v1_response.json README.md docs/API.md
git commit -m "docs: add gaiko2 protocol fixtures and wiring docs"
```

### Task 9: Run verification across both repos

**Files:**
- Modify: none
- Test: `raiko2` and `gaiko2` verification commands

**Step 1: Run focused Rust tests**

Run: `cd <raiko2-checkout> && cargo test -p raiko2-prover gaiko2_protocol_roundtrip gaiko2_adapter gaiko2_client -- --nocapture`

Expected: PASS

**Step 2: Run focused Go tests**

Run: `cd <gaiko2-checkout> && go test ./internal/protocol/... ./internal/prover ./internal/api`

Expected: PASS

**Step 3: Run the `raiko2` server route tests**

Run: `cd <raiko2-checkout> && cargo test -p raiko2 sgx_requests_route_to_shasta_sgx_remote_pipeline -- --nocapture`

Expected: PASS

**Step 4: Run formatting**

Run: `cd <raiko2-checkout> && cargo fmt --all`

Expected: PASS

Run: `cd <gaiko2-checkout> && gofmt -w ./cmd ./internal`

Expected: PASS

**Step 5: Commit**

```bash
cd <raiko2-checkout>
git add .
git commit -m "test: verify gaiko2 shasta v1 integration"
```
