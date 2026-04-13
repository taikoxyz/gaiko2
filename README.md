# gaiko2

`gaiko2` is a lightweight Go service for replaying `raiko2` Shasta execution packets
with `taiko-geth` and producing a TEE proof envelope.

## Baseline

- `/prove/shasta` is implemented with request `schema: "v1"`.
- `gaiko2` can decode `raiko2`-adapted execution packets and replay them with
  native `taiko-geth` stateless execution.
- `gaiko2` validates replay continuity against `proof_carry_data`.
- proof output now supports two signer modes behind one envelope:
  - `native`: sign the final input hash with the fixed GoldenTouch key.
  - `tee`: sign with an enclave-managed key and attach an `ego` quote.
- the checked-in shared fixture under `testdata/` is derived from a real
  `raiko2` GuestInput fixture and replays successfully.

## Verification

Run the current native test suite with:

```bash
cd /home/yue/works/taiko/gaiko2
go test ./...
```

The main replay regression uses:

- [testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json](/home/yue/works/taiko/gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json)
- [internal/prover/replay_fixture_test.go](/home/yue/works/taiko/gaiko2/internal/prover/replay_fixture_test.go)

## Server

Start the service with:

```bash
cd /home/yue/works/taiko/gaiko2
go run ./cmd/gaiko2 server
```

Optional proving configuration:

- `GAIKO2_PROVING_MODE=native|tee`
- `GAIKO2_TEE_TYPE=ego`
- `GAIKO2_SECRET_DIR=/path/to/secrets`
- `GAIKO2_INSTANCE_ID=0xDEADC0DE`

If unset, `gaiko2` defaults to `native` mode.

## Docs

- [Shasta V1 design plan](/home/yue/works/taiko/gaiko2/docs/plans/2026-04-12-gaiko2-shasta-v1-design.md)
- [Shasta V1 implementation plan](/home/yue/works/taiko/gaiko2/docs/plans/2026-04-12-gaiko2-shasta-v1-implementation-plan.md)
- [Current baseline](/home/yue/works/taiko/gaiko2/docs/baselines/2026-04-13-gaiko2-v1-baseline.md)
