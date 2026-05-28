# TDX-Gaiko2 Image Profile Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a first-pass `tdx/` image profile skeleton for `tdxgeth` deployments.

**Architecture:** Keep the profile in the gaiko2 repository. Bake service files and startup scripts into the measured VM image, and export a manifest that hashes binaries plus statement-affecting profile files. Do not introduce an independent image repository.

**Tech Stack:** Bash, systemd, mkosi profile skeleton, JSON manifest

---

### Task 1: Add manifest exporter regression

**Files:**
- Create: `tdx/scripts/test-export-manifest.sh`

**Steps:**
- Create temporary fake binaries.
- Run `tdx/scripts/export-manifest.sh`.
- Assert manifest schema, release tag, component names, statement-affecting files, and TDX measurement fields are present.

**Verification:**
- `tdx/scripts/test-export-manifest.sh` initially fails because the exporter does not exist.

### Task 2: Add TDX image profile files

**Files:**
- Create: `tdx/README.md`
- Create: `tdx/mkosi.conf`
- Create: `tdx/mkosi.extra/etc/gaiko2/tdxgeth.env.example`
- Create: `tdx/mkosi.extra/etc/systemd/system/tdxs.socket`
- Create: `tdx/mkosi.extra/etc/systemd/system/tdxs.service`
- Create: `tdx/mkosi.extra/etc/systemd/system/taiko-geth.service`
- Create: `tdx/mkosi.extra/etc/systemd/system/taiko-client.service`
- Create: `tdx/mkosi.extra/etc/systemd/system/gaiko2-tdxgeth.service`
- Create: `tdx/mkosi.extra/usr/local/bin/taiko-geth-tdx.sh`
- Create: `tdx/mkosi.extra/usr/local/bin/taiko-client-tdx.sh`
- Create: `tdx/mkosi.extra/usr/local/bin/gaiko2-bootstrap-tdx.sh`
- Create: `tdx/mkosi.extra/etc/tdxs/config.yaml`

**Steps:**
- Keep geth HTTP/WS/AuthRPC on loopback.
- Keep gaiko2 remote prover HTTP as the exposed service.
- Keep all mutable state under `/persistent`.
- Use env placeholders rather than checked-in RPC URLs or secrets.

### Task 3: Add manifest export and smoke helpers

**Files:**
- Create: `tdx/scripts/export-manifest.sh`
- Create: `tdx/scripts/smoke.sh`
- Create: `tdx/manifest.example.json`
- Create: `tdx/manifests/.gitignore`

**Steps:**
- Hash the four binaries when paths are provided.
- Hash all systemd units and startup scripts under `tdx/mkosi.extra`.
- Include optional `TDX_IMAGE_ID`, `TDX_MRTD`, and `TDX_RTMR*` fields.
- Keep generated release manifests ignored by default.

### Task 4: Verify and commit

**Verification:**
- `tdx/scripts/test-export-manifest.sh`
- `bash -n tdx/scripts/*.sh tdx/mkosi.extra/usr/local/bin/*.sh`
- `python3 -m json.tool tdx/manifest.example.json`
- `go test ./...`
- `git diff --check`

**Commit:**
- `git commit -m "feat: add tdx image profile"`
