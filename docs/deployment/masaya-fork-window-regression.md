# Masaya Fork-Window Regression Runbook

This runbook explains how to validate `gaiko2` against the Masaya proposal
window `25027..25227`, including the `SHASTA -> UNZEN` transition around
proposal `25127`.

## Goal

Run the real `gaiko2` `/prove/shasta` path over a known Masaya interval and
confirm that:

- every request replays successfully,
- the proof response envelope stays stable,
- the same `instance_address` is returned across the window,
- the fork-transition proposals around `25125..25127` remain healthy.

## What Does And Does Not Need To Change

No `gaiko2` protocol or prover code changes are required for this interval.
Current `main` already provides:

- `GET /healthz`
- `POST /prove/shasta`
- `POST /prove/shasta-aggregate`
- `schema: "v1"` request decoding
- `gaiko2-proof-v1` response encoding

What *is* required is the request-generation side from `raiko2`.

`gaiko2` does not discover proposal tuples or build Shasta packets by itself.
It consumes already-adapted `raiko2` Shasta request JSON and replays it.

## Minimum Gaiko2 Verification

Before touching the interval, verify the local Go surface:

```bash
cd /home/yue/works/taiko/gaiko2
go test ./internal/api ./internal/prover ./internal/protocol ./cmd/gaiko2
```

## Required External Inputs

Use the same live RPCs that the current `raiko2` regression used:

- L2 Masaya RPC: `http://34.41.203.88:8545`
- L1 Hoodi RPC: `https://ethereum-hoodi-rpc.publicnode.com`

The checked-in fixed fork-transition case lives in the `raiko2`
`enable-gaiko2` branch:

- `test/guest_inputs/shasta/taiko_masaya/proposals/proposal_25125.json`
- `test/guest_inputs/shasta/taiko_masaya/proposals/proposal_25126.json`
- `test/guest_inputs/shasta/taiko_masaya/proposals/proposal_25127.json`
- `test/guest_inputs/shasta/taiko_masaya/suites/shasta_unzen_transition.json`

Use those three proposals first as the fixed sanity check before the wider
`25027..25227` interval.

## Start Gaiko2

### Option A: Native Signer Smoke

Use this when you only need replay correctness and envelope shape:

```bash
cd /home/yue/works/taiko/gaiko2
GAIKO2_PROVING_MODE=native \
GAIKO2_FORK=shasta \
GAIKO2_INSTANCE_ID=0xDEADC0DE \
go run ./cmd/gaiko2 server
```

Health check:

```bash
curl http://127.0.0.1:8080/healthz
```

### Option B: Real TEE / SGX

Use this when the goal is real quote-bearing proofs.

The canonical deployment flow is already documented in:

- `docs/deployment/sgx-docker.md`

At minimum, the delegated agent needs to:

1. bootstrap tee state,
2. provide SGX devices,
3. provide `GAIKO2_FORK=shasta`,
4. provide either:
   - `GAIKO2_INSTANCE_ID`, or
   - `registered.gaiko2.json` under `GAIKO2_CONFIG_DIR`,
5. start the server and confirm `GET /healthz`.

If using the local tee Docker image:

```bash
cd /home/yue/works/taiko/gaiko2
./scripts/build-image.sh tee local
```

Then bootstrap:

```bash
docker run --rm \
  --add-host host.docker.internal:host-gateway \
  --device /dev/sgx_enclave \
  --device /dev/sgx_provision \
  -e GAIKO2_PROVING_MODE=tee \
  -e GAIKO2_FORK=shasta \
  -e PCCS_HOST=host.docker.internal:8081 \
  -v /path/to/config:/var/lib/gaiko2/config \
  -v /path/to/secrets:/var/lib/gaiko2/secrets \
  gaiko2-tee:local --init
```

Then serve:

```bash
docker run --rm \
  --add-host host.docker.internal:host-gateway \
  --device /dev/sgx_enclave \
  --device /dev/sgx_provision \
  -e GAIKO2_PROVING_MODE=tee \
  -e GAIKO2_FORK=shasta \
  -e PCCS_HOST=host.docker.internal:8081 \
  -v /path/to/config:/var/lib/gaiko2/config \
  -v /path/to/secrets:/var/lib/gaiko2/secrets \
  -p 8080:8080 \
  gaiko2-tee:local
```

## Generate Requests From Raiko2

All proposal discovery and packet adaptation currently comes from `raiko2`.

### Fixed Fork-Transition Case

Use the three checked-in Masaya fixtures from `raiko2` `enable-gaiko2`:

```bash
cd /home/yue/works/taiko/raiko2
git checkout enable-gaiko2

for id in 25125 25126 25127; do
  cargo run -r -p raiko2-prover --example dump_gaiko2_shasta_fixture -- \
    test/guest_inputs/shasta/taiko_masaya/proposals/proposal_${id}.json \
    /tmp/proposal_${id}.gaiko2-request.json

  curl -sS \
    -H 'content-type: application/json' \
    --data-binary @/tmp/proposal_${id}.gaiko2-request.json \
    http://127.0.0.1:8080/prove/shasta
done
```

Success criteria for each request:

- `schema == "gaiko2-proof-v1"`
- `status == "ok"`
- `result.input` is present
- `result.instance_address` is present
- in tee mode, `result.quote` and `result.public_key` are present

### Full `25027..25227` Interval

First discover proposal tuples from `raiko2`:

```bash
cd /home/yue/works/taiko/raiko2
git checkout enable-gaiko2

python scripts/regression/stress_shasta_proposal.py \
  --network taiko_masaya \
  --l1-network hoodi \
  --l2-rpc http://34.41.203.88:8545 \
  --l1-rpc https://ethereum-hoodi-rpc.publicnode.com \
  --proposal-ids "$(seq -s, 25027 25227)" \
  --discover-only \
  --proposal-out /tmp/masaya-25027-25227.discovery.json
```

Then for each discovered proposal:

1. run `preflight --proof-type sgx --validate`,
2. convert the resulting `GuestInput` with `dump_gaiko2_shasta_fixture`,
3. `POST` the adapted JSON to `gaiko2`.

The command shape for one proposal is:

```bash
cargo run -r -p preflight -- \
  --network taiko_masaya \
  --l1-network hoodi \
  --rpc-url http://34.41.203.88:8545 \
  --l1-rpc-url https://ethereum-hoodi-rpc.publicnode.com \
  --proposal-id <proposal-id> \
  --l1-inclusion-block-number <l1-inclusion-block-number> \
  --last-anchor-block-number <last-anchor-block-number> \
  --l2-start <l2-start> \
  --l2-end <l2-end> \
  --proof-type sgx \
  --validate \
  --pretty \
  --output /tmp/proposal-<proposal-id>.guest-input.json

cargo run -r -p raiko2-prover --example dump_gaiko2_shasta_fixture -- \
  /tmp/proposal-<proposal-id>.guest-input.json \
  /tmp/proposal-<proposal-id>.gaiko2-request.json

curl -sS \
  -H 'content-type: application/json' \
  --data-binary @/tmp/proposal-<proposal-id>.gaiko2-request.json \
  http://127.0.0.1:8080/prove/shasta
```

## Expected Response Shape

The current healthy response looks like:

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

In `native` mode, `quote` may be omitted. In tee mode, `quote` and
`public_key` should be present.

## Recommended Division Of Work

If this is delegated to another agent, split responsibilities like this:

### Gaiko2 Owner

- keep `gaiko2` on latest `main`
- run `go test ./internal/api ./internal/prover ./internal/protocol ./cmd/gaiko2`
- start `gaiko2` in the desired mode
- verify `/healthz`
- collect failed `/prove/shasta` responses if any

### Raiko2 Owner

- discover proposal tuples
- build `GuestInput`
- adapt each `GuestInput` into a `gaiko2` request JSON
- maintain the fixed fork-transition fixture suite around `25125..25127`

## Current Recommendation

For the first delegated pass:

1. run the fixed `25125/25126/25127` case first,
2. then run the full `25027..25227` interval,
3. stop immediately on the first non-`200` or non-`ok` response,
4. save the failing request JSON and the exact proposal tuple.

That gives the narrowest reproduction if anything regresses around the Masaya
fork boundary.
