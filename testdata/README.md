# Shared Shasta Fixtures

`shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json` is the
canonical checked-in server fixture. It uses request
`schema: "raiko2-shasta-request-v1"` and carries the full `guest_input`.

The fixture is derived from the tracked `raiko2` shared GuestInput fixture:

- source:
  [raiko2/tests/fixtures/shasta_guest_input_taiko_mainnet_proposal_2222_l2_5412225_5412416.json](https://github.com/taikoxyz/raiko2/blob/main/tests/fixtures/shasta_guest_input_taiko_mainnet_proposal_2222_l2_5412225_5412416.json)

Regenerate the server fixture from the canonical `raiko2` adapter with:

```bash
cd <raiko2-checkout>
cargo run -p raiko2-prover --no-default-features --example dump_gaiko2_shasta_fixture -- \
  tests/fixtures/shasta_guest_input_taiko_mainnet_proposal_2222_l2_5412225_5412416.json \
  ../gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json
```

The historical replay-only v1 fixture was removed. Current v1 proposal requests
must include `payload.guest_input`.
