# gaiko2 Security Audit — Soundness Review (kimi-k3)

**Date:** 2026-07-22
**Scope:** Adversarial soundness review of the gaiko2 TEE prover, focused on bugs that could produce a proof for a fake or incorrect block hash / state transition accepted by the Taiko Shasta Inbox.
**Method:** Three independent adversarial review agents (derivation/manifest validation, execution/replay, hashing/signing/aggregation), each candidate finding then independently verified or refuted by a separate verification agent with code-level evidence, including inspection of the pinned `taiko-geth` dependency and the on-chain reference contracts (`SgxVerifier.sol`, `LibPublicInput`, `LibHashOptimized`) and the canonical derivation rules (`taiko-client-rs`, `Derivation.md`).

---

## 1. Executive summary

gaiko2 takes a `GuestInput` (witness blocks, blobs, proposal event data, and a `proof_carry_data` claim), validates it against the Taiko Shasta derivation rules, re-executes the blocks statelessly with taiko-geth, and signs a digest that the L1 Inbox/verifier trusts. If any check is missing, a malicious prover can get a valid signature over a block hash or state root that the canonical Taiko node software would never produce — i.e., a fake proof.

The review found **one High** and a small number of Medium/Low issues. No unmitigated Critical issue was confirmed on the mainnet path, but **one Critical-severity configuration hazard** exists for any deployment that registers the built-in mock signer on-chain (devnets, testnets, or misconfigured verifiers).

| # | Title | Severity | Status |
|---|-------|----------|--------|
| 1 | Missing `statedb.Error()` check in the block replay path allows read-path witness forgery | **High** | Confirmed |
| 2 | Native proving mode signs with a published private key; aggregate endpoint is a forge oracle wherever the mock instance is trusted | **Critical (config-dependent)** | Confirmed |
| 3 | The GoldenTouch anchor key is reused as the native proof-signing key and is published in the repo | **High (hardening)** | Confirmed |
| 4 | Silent fallback to native mode when `GAIKO2_PROVING_MODE` is unset | Medium | Confirmed |
| 5 | Verifier address / chain spec taken from requester JSON with no cross-witness consistency check | Low | Confirmed (attack refuted for supported chains) |
| 6 | Unzen zk-gas truncation / tx-filter classifier must exactly match the block producer; drift yields non-canonical blocks | Medium (latent) | Confirmed by design |
| 7 | `Anchored` event's `anchorBlockHash` word is never cross-checked against validated anchor calldata | Low | Confirmed |
| 8 | `u48Word` truncates instead of rejecting out-of-range values | Low | Confirmed (currently unreachable) |
| 9 | `witness.accounts` is required and pinned but never used | Low / informational | Confirmed |

Refuted candidates (checked and cleared): RLP trailing-byte leniency in manifest decoding (the pinned taiko-geth `rlp.DecodeBytes` rejects trailing data, matching the canonical Rust `decode_exact`); the single-proof 4-word hash not matching the on-chain 5-word digest (intended protocol layering — the single-proof hash is only an intermediate consumed by the aggregation endpoint, which produces the on-chain digest); L1 ancestor header forgery (the chain is transitively bound to the proposal's real `originBlockHash`).

---

## 2. Findings

### Finding 1 (High): Missing `statedb.Error()` check in the replay execution path — read-path witness forgery

**Files:** `internal/prover/replay.go:51-88` (`GethRunner.Execute`), `replay.go:90-212` (`processReplayBlock` / `processUnzenReplayBlock`), `replay.go:317-324` (`ReplayService.Prove`)

**Background.** gaiko2 executes blocks *statelessly*: all state comes from a prover-supplied "witness" (a bag of trie nodes). go-ethereum's `StateDB` does not abort when a trie node is missing during a **read** — it records the error in a deferred slot (`db.Error()`) and returns a **zero value**. The error only reliably surfaces if the caller explicitly checks `statedb.Error()` (or calls `Commit`, which the stateless path never does).

**The bug.** Nowhere in the actual block-execution path is `statedb.Error()` checked:

- `GethRunner.Execute` calls `core.BlockValidator.ValidateState(block, db, res, true)` with `stateless=true` (`replay.go:74`). In the pinned dependency (`taiko-geth v1.18.1-0.20260709024242-8f5abc6d0636`, `core/block_validator.go:144-148`), stateless mode **early-returns before** the state-root comparison and before the only `statedb.Error()` check.
- `db.IntermediateRoot()` (`replay.go:82`) does not fail on the deferred read error either; it only hashes *dirty* (written) state.
- Crucially, the codebase already knows about this exact hazard: `internal/prover/l2_state.go:61-69` and `internal/prover/manifest_tx_filter.go:204-206, 250-252, 296-301` **do** check `statedb.Error()`, with a comment stating that a missing witness node "cannot masquerade as a legitimately empty storage slot". The replay path simply lacks the same guard.

**Attack scenario.** A malicious prover constructs a non-canonical block whose transactions **read but never write** a particular storage slot or account (e.g. a flag gating an `if` in a contract, a balance threshold). The supplied witness omits the trie nodes for that slot. During replay, the read silently returns zero, execution takes the "slot is empty" branch (diverging from what a canonical node with the real state would compute), and the execution still produces a deterministic, self-consistent post-state root, receipt root, bloom, and gas usage — all of which the attacker embeds in the forged header. Since untouched read-only paths leave no trace in `IntermediateRoot`, the forged root matches the forged header, and `Prove` accepts it (`replay.go:317`). The result is a signature over a state transition no canonical client would compute.

The attack requires the attacker to also control the proposal/carry chain (the proposal hash, parent block hash, and anchor linkage are all bound to the L1 proposal), so this is a **proposer/prover equivocation or mis-execution window** — exactly the threat model a prover is supposed to defend against. Independent verification confirmed the mechanics end-to-end against the pinned dependency.

**Recommendation.** Check `db.Error()` immediately after `processReplayBlock` in `GethRunner.Execute` (before and after `IntermediateRoot`), mirroring the existing guards in `l2_state.go` / `manifest_tx_filter.go`. One-line fix; also consider the same check inside `processUnzenReplayBlock` after the transaction loop.

---

### Finding 2 (Critical, configuration-dependent): Native mode + aggregate endpoint = proof-forgery oracle with a published key

**Files:** `internal/prover/aggregate.go:12-107`, `internal/prover/signer.go:81-128`, `internal/prover/replay.go:25-30`, `internal/api/server.go:25-155`, `cmd/gaiko2/main.go:149-155`

**Background.** The `/prove/shasta-aggregate` endpoint takes a list of subproofs (carry + signature) and produces the final signature that the L1 `SgxVerifier` accepts. It executes **no blocks** — its only soundness guarantee is that every subproof signature recovers to *this service's own instance address*, on the theory that this key only ever signs subproofs via the honest `Prove` path.

**The bug.** The signing key in `native` mode is a **hard-coded private key published in this repo** (`nativeProofPrivateKey = 9295...ce38`, instance `0xDEADC0DE`, `replay.go:28-29`). The corresponding address is the well-known Taiko GoldenTouch account `0x0000777735367b36bC9B61C50022d9D0700dB4Ec`. Both proving endpoints are **unauthenticated** (`server.go` — plain `http.ServeMux`, no middleware).

**Attack scenario.** Against any gaiko2 deployment running in native mode whose instance address is registered in the target `SgxVerifier` (devnets and internal testnets do this deliberately; the repo README and runbooks instruct `GAIKO2_PROVING_MODE=native` + `GAIKO2_INSTANCE_ID=0xDEADC0DE`), an attacker:

1. fabricates arbitrary, internally-consistent proof carries (fake proposal hashes, fake state roots, arbitrary `actualProver`) — passing the continuity checks in `aggregate_validate.go:104-138`;
2. signs each carry's subproof hash themselves with the published key — passing `validateAggregateProofSignatures`;
3. POSTs to `/prove/shasta-aggregate` and receives a valid signature over the exact 5-word `VERIFY_PROOF‖chainId‖verifier‖commitmentHash‖instance` digest the on-chain verifier accepts.

Result: finalization of fabricated L2 state. No TEE is involved at any step.

**Nuances from verification.** (a) On mainnet this requires the verifier owner to have registered the GoldenTouch/mock instance — an operator action, so mainnet exploitability is a misconfiguration hazard rather than a default. (b) An unknown/typo'd proving mode fails closed at startup (`signer.go:107-108`); only the **empty/unset** case silently degrades (Finding 4).

**Recommendation.** Refuse to serve `/prove/shasta-aggregate` (and log a loud startup warning) in native mode unless an explicit `--dev`/`GAIKO2_DEV_MODE` flag is set. Document that the mock instance must never be registered in any verifier guarding real value.

---

### Finding 3 (High, hardening): The GoldenTouch anchor key doubles as the native proof-signing key, and it is published

**Files:** `internal/prover/manifest_validate.go:36-45`, `internal/prover/replay.go:28`, `internal/prover/signer.go:114-128`

`shastaGoldenTouchPrivateKey` and `nativeProofPrivateKey` are the **same key**. The GoldenTouch key signs L2 anchor transactions; here it also signs native-mode proofs. Publishing an anchor-signing key is by design on Taiko (the anchor tx form is canonicalized), but reusing the same key for *proof signing* broadens its blast radius: anyone holding the repo can produce both valid anchor transactions and valid native-mode proof signatures, and Finding 2 turns the latter into forged proofs wherever the instance is trusted. The keys should be distinct, and the proof-signing key for any non-throwaway deployment should live only inside the enclave.

---

### Finding 4 (Medium): Silent fallback to native proving mode

**Files:** `internal/prover/signer.go:82-85`, `cmd/gaiko2/main.go:149-155`

If `GAIKO2_PROVING_MODE` is unset, the service silently boots in `native` mode (the only signal is a `mode=native` startup log line). Combined with Findings 2–3, a single missing environment variable converts a production prover into a forge oracle using a published key. Unknown values do fail closed. Recommendation: require an explicit mode, and fail startup if native mode is selected without an explicit dev opt-in.

---

### Finding 5 (Low): Verifier address and chain spec come from requester JSON, first witness only

**Files:** `internal/prover/guestinput_carry.go:351-401` (`resolveGuestInputVerifier`), `guestinput_carry.go:423-499`, `internal/prover/validate.go:117-128`

The verifier address embedded in the signed digest is resolved from `witnesses[0].chain_spec.verifier_address_forks.<fork>.SgxGeth` — attacker-supplied JSON — and only checked for self-consistency with `carry.Verifier`. Only the **first** witness's chain spec is consulted; the remaining witnesses' `chain_spec` values are never compared for consistency. Independent verification **refuted** the dangerous version of this attack for all chains gaiko2 can actually execute: `chainConfigFor` (`replay.go:735-779`) is a closed whitelist of hard-coded network IDs/fork schedules, and the proposal hash, blob hashes, anchor signature, and parent block hash bindings tie every proof to canonical history — a digest naming an unauthorized verifier is rejected on-chain. Residual risk is limited to fork rotations and misconfigured multi-verifier deployments. Recommendation: resolve the verifier and chain ID per witness and require agreement across all witnesses.

---

### Finding 6 (Medium, latent): Non-canonical block risk from zk-gas truncation / tx-filter drift

**Files:** `internal/prover/replay.go:149-212` (`processUnzenReplayBlock`), `replay.go:803-808` (`unzenZkGasScheduleFor`), `internal/prover/manifest_tx_filter.go:21-289`

For Unzen blocks, gaiko2 *reconstructs* the block it then "verifies": it filters the manifest transaction list through a hard-coded mirror of the taiko-geth recoverable-error classifier, applies zk-gas truncation with `vm.UnzenZkGasSchedule` (noting an upstream schedule reset, taiko-geth #569), and requires the resulting tx root / difficulty to match the supplied header. This is internally consistent, but the only external binding is the manifest/metadata field checks — the actual tx selection depends on gaiko2's classifier and gas schedule **exactly matching the real block producer's** (alethia-reth / taiko-client-rs) for the given fork epoch. Any drift — a recoverable-error rule change, a schedule change — silently makes gaiko2 prove a block whose transaction set (and hence state root and block hash) differs from what the canonical network produced. There is no in-repo fix beyond diligence: the classifier and schedule must be diffed against the exact producer implementation per fork, ideally with differential tests against canonical fixtures.

Related latent hazard (verified, Low): `enableUnzenForksFrom` (`replay.go:782-801`) fills `Cancun/Prague/Osaka` times from the Unzen time only when the upstream config leaves them nil; a future taiko-geth bump that populates them with different values would silently change post-Unzen fork gating (`ProcessParentBlockHash`, deposit/withdrawal/consolidation queues, requests-hash). Consider asserting the expected fork times rather than inheriting upstream defaults.

---

### Finding 7 (Low): `Anchored` event's L1 block hash word is never cross-checked

**Files:** `internal/prover/replay.go:465-501` (`replayAnchoredEvent`), cross-reference `internal/prover/manifest_validate.go:1160-1186`, `1337-1438`

The anchor-continuity chain across blocks tracks only the first two words of the `Anchored(uint48,uint48,bytes32)` event (`prevAnchorBlockNumber`, `anchorBlockNumber`). The third word — the L1 block hash — is parsed for nothing. The L1 linkage *is* enforced independently via the `anchorV4` calldata checkpoint (`validateAnchorL1Linkage`), so this is not currently exploitable; but if the emitted event and the stored checkpoint ever diverge (contract upgrade, changed semantics), the two tracking paths would disagree silently. Recommendation: assert `event.anchorBlockHash == anchorV4 calldata blockHash` (and likewise for the block number).

---

### Finding 8 (Low): `u48Word` silently truncates

**File:** `internal/prover/hash.go:184-186`

`u48Word(v) = u64Word(v & 0xffffffffffff)` masks instead of rejecting. If a value exceeding uint48 ever reached a signing path, gaiko2 would sign the digest of the *truncated* value — which equals what the on-chain verifier computes for the truncated value. Today all decode sites reject out-of-range values first (`requireUint48`), so this is defense-in-depth; but the invariant lives at decode sites, not at the hashing/signing sites. Recommendation: make `u48Word` reject (or panic on) out-of-range input.

---

### Finding 9 (Low / informational): `witness.accounts` is required, pinned, and never used

**Files:** `internal/prover/guestinput.go:127-136`, `internal/prover/replay.go:622-659`, `internal/prover/decode.go:558-583`, `internal/prover/validate.go:117-128`

Every witness must include an `accounts` payload, which is byte-pinned into the validation binding — yet nothing in the replay path parses or applies it; executed state comes exclusively from the witness trie. A host can supply an `accounts` map that contradicts the witness without affecting anything today, but it is an unenforced invariant that could mislead any future code path that trusts it. Either cross-check `accounts` against the witness trie or stop requiring it.

---

## 3. Refuted candidates (verified non-issues)

- **RLP trailing-byte leniency in manifest decoding.** Claimed that `rlp.DecodeBytes` might accept trailing junk, diverging from the canonical Rust `decode_exact`. **Refuted:** the pinned `taiko-geth v1.18.1-0.20260709024242-8f5abc6d0636` `rlp.DecodeBytes` explicitly rejects trailing data (`ErrMoreThanOneValue`), and decode failure degrades the source to the default manifest exactly as the canonical driver does (`manifest_validate.go:362-365`).
- **Single-proof hash ≠ on-chain digest.** `hashShastaSubproofCarry` (4 words, no instance) indeed differs from the on-chain 5-word `VERIFY_PROOF` digest — but this is intended layering: the single-proof hash is only consumed by the aggregation endpoint, which recomputes the canonical 5-word digest from the carries. The aggregation hash and commitment encoding were verified word-for-word against `LibPublicInput.hashPublicInputs` / `LibHashOptimized.hashCommitment`.
- **Forged L1 ancestor headers in anchor linkage.** The prover supplies `l1_ancestor_headers`, but the tip must equal the proposal's real `originBlockHash`, and each header hash commits to its parent — so the entire supplied chain is transitively bound to real L1 history. No forgery gap.
- **Blob KZG binding.** Blob bytes are bound to the proposal's versioned hashes by recomputing `BlobToCommitment` + `CalcBlobHashV1` (`blob_validate.go:70-99`); point proofs are unnecessary for this binding. Sound.

---

## 4. What was verified as sound (selected)

Beyond the refuted items above, the reviewers verified, line-by-line against the canonical `taiko-client-rs` derivation and the on-chain contracts, with no gaps found:

- Proposal hashing matches `LibHashOptimized.hashProposal` (field order, uint widths, dynamic `sources` encoding); width checks reject rather than truncate at decode time.
- Aggregation continuity checks (proposal ID +1 chain, proposal-hash chain, checkpoint-block-hash chain, chainID/verifier/actualProver constancy) and instance binding.
- Manifest/anchor validation: GoldenTouch fixed-k signature re-derivation, exact `anchorV4` calldata shape, anchor number progression and max-offset bounds, timestamp window bounds, gas-limit ±200ppm bounds, forced-inclusion ordering, and default-manifest degradation — all equivalent to `validation.rs` / `Anchor.sol`.
- Cross-block state continuity: per-block witnesses are bound to headers that hash-chain from the Inbox-committed parent, so the proven sequence is one continuous transition; independent-witness forgery is not possible.
- The validation-time binding (`sealValidatedRequest` / `validateRequestSigningBinding`) pins the carry, decoded blocks, and all raw block/chain-spec/witness/accounts bytes, and `Prove` re-derives and re-executes from the pinned bytes before signing.

## 5. Suggested priority

1. **Finding 1** — add the `statedb.Error()` check in the replay path (small, surgical, closes the only confirmed execution-layer soundness gap).
2. **Findings 2–4** — gate native mode behind an explicit dev flag, refuse aggregate signing in native mode by default, and split the proof-signing key from the GoldenTouch key.
3. **Finding 6** — establish a process (and differential fixtures) to keep the zk-gas schedule and tx-filter classifier in lockstep with the canonical producer per fork.
4. Findings 5, 7, 8, 9 — cheap hardening and invariant cleanups.
