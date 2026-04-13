# Shared Shasta Replay Fixture

`shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json` is a checked-in
`gaiko2-shasta-v1` execution packet derived from the tracked `raiko2` shared GuestInput fixture:

- source:
  `/home/yue/works/taiko/raiko2/tests/fixtures/shasta_guest_input_taiko_mainnet_proposal_2222_l2_5412225_5412416.json`

Regenerate it from the canonical `raiko2` adapter with:

```bash
cd /home/yue/works/taiko/raiko2
cargo run -p raiko2-prover --no-default-features --example dump_gaiko2_shasta_fixture -- \
  tests/fixtures/shasta_guest_input_taiko_mainnet_proposal_2222_l2_5412225_5412416.json \
  ../gaiko2/testdata/shasta_request_taiko_mainnet_proposal_2222_l2_5412225_5412416.json
```
