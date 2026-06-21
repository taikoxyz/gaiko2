# Gaiko2 Shasta V1 Design Plan

**Goal:** Build a lightweight `gaiko2` service that cross-checks `raiko2` Shasta execution by replaying canonical witnesses with `taiko-geth` inside TEE, without rebuilding the full `raiko2` preflight stack in Go.

## Context

Historically, `raiko + gaiko` were used to cross-check `reth` and `geth` EVM behavior. For `raiko2`, we need an equivalent `gaiko2`.

The main design question was whether `gaiko2` must:

1. Independently implement preflight, blob/driver/proposal derivation, and witness construction from `taiko-geth`, or
2. Reuse canonical witness data produced by `raiko2` and focus on `geth` stateless replay plus TEE proof generation.

## Facts Confirmed During Analysis

### 1. Old `gaiko` is a witness consumer, not an independent preflight engine

The existing `gaiko` accepts `GuestInput` / `GuestBatchInput` JSON, reconstructs a geth stateless witness, executes with `core.ExecuteStateless`, and derives the final public input and SGX proof. It does not independently derive the full witness from chain RPC.

### 2. `raiko2` already has a canonical preflight pipeline

`raiko2` currently:

- fetches blocks and witnesses from a witness-capable L2 RPC,
- derives canonical Shasta proposal context and manifest data,
- validates anchor and proposal continuity,
- builds `GuestInput` as `Vec<StatelessInput> + TaikoManifest + proof_carry_data`.

### 3. Reusing `raiko2` witness does not eliminate execution cross-check

This was the key technical uncertainty.

The answer is:

- `gaiko2` would still perform an independent `geth` stateless execution from the pre-state root,
- `taiko-geth` reconstructs the state DB from hashed witness nodes, code blobs, and ancestor headers,
- `geth` then executes the block and recomputes post-state root and receipt root itself,
- therefore, `reth` execution bugs can still be detected even if the witness came from the `raiko2` side.

What is **not** independently checked when reusing `raiko2` witness:

- witness generation logic,
- proposal/blob/anchor derivation,
- Shasta manifest construction,
- higher-level preflight semantics.

So witness reuse preserves **execution independence**, but not **preflight independence**.

## Decision

For `gaiko2` v1, choose:

**Reuse canonical witness data from `raiko2`, and make `gaiko2` a lightweight execution cross-check + TEE proving service.**

This means:

- `raiko2` remains the single source of truth for canonical preflight,
- `gaiko2` receives a stable execution packet,
- `gaiko2` replays it with `taiko-geth`,
- `gaiko2` computes the final public input from `proof_carry_data`,
- `gaiko2` returns a TEE proof envelope back to `raiko2`.

## Why This Is The Right V1

### Benefits

- Keeps the cross-EVM value we actually need first: `reth` vs `geth` execution agreement.
- Avoids reimplementing the most fragile and protocol-heavy part of `raiko2` immediately.
- Minimizes time-to-first-working-PoC.
- Preserves a clean upgrade path to a future fully independent preflight engine.

### Costs

- `gaiko2` v1 will not independently validate `raiko2` witness generation.
- `gaiko2` will trust `raiko2` for Shasta preflight semantics.

These are acceptable for v1 because the core purpose is execution cross-check, not full pipeline duplication.

## System Boundary

### `raiko2` responsibilities

- canonical Shasta/Uzen preflight,
- canonical witness fetching and validation,
- canonical `proof_carry_data` generation,
- conversion from internal `GuestInput` into a stable `gaiko2` execution packet,
- HTTP client submission to `gaiko2`,
- mapping `gaiko2` proof responses back into canonical `raiko2::Proof`.

### `gaiko2` responsibilities

- accept versioned internal HTTP packets,
- validate packet schema and continuity,
- replay every block with `taiko-geth` stateless execution,
- verify final checkpoint continuity against `proof_carry_data`,
- derive final public input hash,
- sign and attest inside TEE,
- return a stable proof response.

## Stable Internal Protocol

To avoid repeating the old `raiko`/`gaiko` upgrade breakage, `gaiko2` must not consume raw `raiko2` internal structs.

Instead, define a stable JSON envelope:

```json
{
  "schema": "gaiko2-shasta-v1",
  "payload": {
    "chain_id": 167013,
    "blocks": [
      {
        "block": { "...": "..." },
        "witness": {
          "state": ["0x..."],
          "codes": ["0x..."],
          "headers": ["0x..."]
        }
      }
    ],
    "proof_carry_data": { "...": "..." }
  }
}
```

### Why this packet is intentionally small

`gaiko2` only needs:

- the canonical replay blocks,
- the stateless witness for those blocks,
- enough proof-carry metadata to derive the final public input and continuity checks.

It does **not** need the full `TaikoManifest`, proposal event, or other preflight internals.

## Response Protocol

`gaiko2` returns a stable proof envelope:

```json
{
  "schema": "gaiko2-proof-v1",
  "status": "ok",
  "result": {
    "proof": "0x...",
    "quote": "0x...",
    "public_key": "0x...",
    "instance_address": "0x...",
    "input": "0x..."
  }
}
```

Error responses must also be schema-stable.

## Transport

Use internal HTTP in v1.

Reasons:

- matches the historical `raiko -> gaiko` deployment model,
- naturally fits long-lived TEE service processes,
- simpler for health checks, remote deployment, retries, and timeouts than CLI/stdin.

Recommended route:

- `POST /internal/prove/shasta-proposal`

## `raiko2` Integration Shape

Treat `gaiko2` as a new prover route, not as a mutation of `native`.

Recommended high-level route shape:

- external request stays `proof_type=sgx`,
- internal route becomes effectively `sgx/remote`,
- `raiko2` constructs a dedicated `Gaiko2Prover`,
- existing `ShastaSpec` / engine pipeline stays intact.

This keeps the change localized to:

- prover config / route mapping,
- engine factory registration,
- new prover implementation,
- response mapping.

## `gaiko2` Internal Module Shape

Keep `gaiko2` lightweight.

Recommended packages:

- `internal/api`
- `internal/protocol`
- `internal/adapter`
- `internal/prover`
- `internal/tee`

The service should not replicate `raiko2`'s manifest, provider, or pipeline structure.

## Mandatory `gaiko2` Validation

Even in PoC mode, `gaiko2` must reject malformed packets early.

Required checks:

- `schema == gaiko2-shasta-v1`,
- non-empty blocks,
- contiguous block numbers,
- non-empty witness headers,
- successful geth stateless replay for each block,
- replayed state root and receipt root match each block header,
- `proof_carry_data.transition_input.parent_block_hash` matches first block parent hash,
- `proof_carry_data.transition_input.checkpoint` matches the last block number/hash/state root,
- payload `chain_id` matches `proof_carry_data.chain_id`.

## API Stability Rule

Do not use the `raiko2` binary version as compatibility control.

Use only the packet `schema` value as the wire contract.

Rules:

- additive, backward-compatible changes stay inside `gaiko2-shasta-v1`,
- breaking changes create `gaiko2-shasta-v2`,
- `gaiko2` must reject unknown schema values,
- `raiko2` must send an explicit schema and never infer one.

## Non-Goals For V1

V1 explicitly does **not** include:

- fully independent `gaiko2` preflight from `taiko-geth`,
- blob/driver/proposal derivation from Go,
- independent Shasta manifest construction,
- separate `sgxgeth` public API naming,
- protobuf or code-generated schema tooling,
- aggregation support before proposal proving is stable.

## Future V2 Direction

If later we need full end-to-end independence from `raiko2` preflight, then `gaiko2` v2 can add:

- independent block/proposal/blob derivation,
- independent witness construction from `taiko-geth`,
- independent manifest and continuity reconstruction,
- cross-checks for preflight as well as execution.

### TDX derived-input follow-up

For the TDX path, prefer a bounded v2 endpoint where the untrusted host still
provides blob/calldata payloads, proposal source metadata, parent context, and
witnesses. The TDX service should verify those inputs and then derive the
candidate txlist internally:

- verify blob or calldata payloads against proposal source metadata,
- decompress and RLP-decode the txlist inside TDX,
- prepend or validate the anchor transaction from the supplied parent/proposal
  context,
- execute the candidate txlist with taiko-geth's exported prover helper so
  invalid transaction filtering and zk-gas truncation reuse the canonical geth
  rules,
- assemble the filtered block from the actually committed transactions,
- replay/check the filtered block with the existing gaiko2 stateless execution
  path before signing.

Do not reimplement invalid transaction filtering or zk-gas truncation directly
in gaiko2. If taiko-geth does not expose the needed helper, add that export
first and treat this as a v2 task rather than extending the current v1 replay
packet implicitly.

That is a separate project phase and should not block v1.

## Final Recommendation

Proceed with `gaiko2` v1 as:

**`raiko2 canonical preflight` -> `stable execution packet adapter` -> `gaiko2 geth replay + TEE proof`**

This gives us the execution-level cross-check we need now, keeps the protocol surface stable, and leaves room for a future independent-preflight `gaiko2` without redoing the v1 architecture.
