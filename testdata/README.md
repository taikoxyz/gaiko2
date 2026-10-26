# Proposal Fixtures

## Retained local legacy fixture

`shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json` is the
retained local legacy unit and protocol fixture. It uses request
`schema: "raiko2-shasta-request-v1"` and carries the full `guest_input`.

It was adapted from a historical Raiko GuestInput and predates Unzen. Keep its
data and existing tests unchanged. Shared offline upgrade regressions use the
external Unzen-or-later fixtures described below.

The historical replay-only v1 fixture was removed. Current v1 proposal requests
must include `payload.guest_input`.

## External offline proposal regressions

[zkgas-bench](https://github.com/taikoxyz/zkgas-bench) owns the shared Unzen-or-later
offline GuestInput fixtures, provenance, checksums, and semantic expected results.
Gaiko owns request adaptation, case selection, replay, and assertions. Acquire and
verify a fixed fixture revision using its README before running the test; the
test never downloads data. Keep downloaded inputs and adapted requests outside
this repository. The checked-in unit fixture above remains hermetic and does not
require access to the external repository.

First adapt the verified canonical GuestInput using Raiko's existing adapter:

```bash
cd <raiko2-checkout>
cargo run -p raiko2-prover --no-default-features --example dump_gaiko2_shasta_fixture -- \
  ../fixture-cache/guest-input.json \
  ../fixture-cache/gaiko-request.json
```

The adapter materializes shared witnesses into Gaiko's request representation.
Do not merely wrap raw GuestInput JSON in a request envelope. Preserve the
fixture's original `proof_carry_data`, including its prover and verifier context;
changing carry changes the public commitment.

Use the fixed fixture checkout's recorded expected proposal public commitment
file directly, or a byte-for-byte copy verified against that revision. For
[mainnet-unzen-proposal-41455-v1](https://github.com/taikoxyz/zkgas-bench/tree/main/fixtures/regressions/mainnet-unzen-proposal-41455-v1),
the file is
`fixtures/regressions/mainnet-unzen-proposal-41455-v1/expected/public-output.txt`.
It contains one `0x`-prefixed, 32-byte hexadecimal value. With the fixed fixture
checkout at `../zkgas-bench`, run the opt-in native regression from Gaiko:

```bash
cd <gaiko2-checkout>
fixture_dir="$(cd ../fixture-cache && pwd)"
fixture_case="$(cd ../zkgas-bench/fixtures/regressions/mainnet-unzen-proposal-41455-v1 && pwd)"
GAIKO2_OFFLINE_REGRESSION_REQUEST="$fixture_dir/gaiko-request.json" \
GAIKO2_OFFLINE_REGRESSION_EXPECTED_OUTPUT_FILE="$fixture_case/expected/public-output.txt" \
  go test -count=1 ./internal/prover -run '^TestOfflineProposalFixture$'
```

Use absolute environment paths because Go runs the test from its package
directory. With both variables unset, the test skips. Empty or incomplete
configuration, unreadable files, invalid JSON or requests, malformed commitments,
replay errors, and a `result.input` mismatch fail the test. A wrong, well-formed
expected commitment must fail even when replay succeeds.

The test explicitly selects native mode regardless of ambient service settings.
It runs real stateless execution and produces a mock signature with the published
native key. This is execution plus mock signing, not a TEE proof: it loads no TEE
keys, contacts no RPC, and submits no proofs. Compare the semantic public
commitment, not proof bytes or performance measurements. Online regression may
follow current software and chain data separately from these frozen inputs.
