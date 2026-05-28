# TDX-Gaiko2 Image Profile

This directory contains the TDX VM image profile for `gaiko2` `tdxgeth` mode.
It is a deployment profile attached to gaiko2 releases, not an independent
project or API dependency for raiko2.

The measured image should bake in:

- `/usr/bin/gaiko2`
- `/usr/bin/taiko-geth`
- `/usr/bin/taiko-client`
- `/usr/bin/tdxs`
- the systemd units under `tdx/mkosi.extra/etc/systemd/system`
- the startup scripts under `tdx/mkosi.extra/usr/local/bin`
- the config templates under `tdx/mkosi.extra/etc`

Do not build accepted TDX provider images from runtime-pulled containers,
floating tags, or host-mounted startup scripts. Those patterns make the TDX
measurement weaker than the proof statement.

## Runtime

The intended service graph is:

```text
tdxs.socket
tdxs.service
taiko-geth.service
taiko-client.service
gaiko2-tdxgeth.service
```

`taiko-geth` binds JSON-RPC, WebSocket, and AuthRPC to loopback only. `gaiko2`
uses `http://127.0.0.1:8545` to check headers before signing the raiko2 remote
prover input hash. The only service that needs to be reachable by raiko2 is the
gaiko2 HTTP server.

## Build Shape

`mkosi.conf` now defines the first buildable image profile. The build expects
prebuilt `taiko-geth`, `taiko-client`, and `tdxs` binaries, and builds `gaiko2`
from this repository unless `GAIKO2_BIN` is provided.

```bash
TAIKO_GETH_BIN=/path/to/taiko-geth \
TAIKO_CLIENT_BIN=/path/to/taiko-client \
TDXS_BIN=/path/to/tdxs \
tdx/scripts/build-image.sh
```

The profile creates users/groups, enables the fixed systemd services, installs
the four binaries, and runs `runtime-init` before the prover/node services.

This is still not the final production-hardening layer. Before production, add:

- verified TDX-compatible kernel/initramfs/firmware selection for the target
  cloud or bare-metal host
- rootfs immutability or dm-verity
- RTMR[3] extension from the manifest/event log before `gaiko2 bootstrap`
- cloud-specific custom image import/start automation

## Manifest

Generate a release manifest after binaries are available:

```bash
RELEASE_TAG=0.2.0-tdx1 \
GAIKO2_BIN=/path/to/gaiko2 \
TAIKO_GETH_BIN=/path/to/taiko-geth \
TAIKO_CLIENT_BIN=/path/to/taiko-client \
TDXS_BIN=/path/to/tdxs \
TDX_IMAGE_ID=0x... \
TDX_MRTD=0x... \
tdx/scripts/export-manifest.sh
```

Use `STRICT=1` in release automation to fail when any required binary or
measurement field is missing.

Generated manifests are written under `tdx/manifests/` and ignored by default.
Upload the generated manifest with the VM image artifact and use its measurement
fields in provider registration.

## Functional SSH Test

Before the clean image flow is ready, use an existing TDX-capable GCE VM only for
functional validation:

1. Install or copy the same four binaries into the VM.
2. Copy the files under `tdx/mkosi.extra` into the matching absolute paths.
3. Copy `tdx/mkosi.extra/etc/gaiko2/tdxgeth.env.example` to
   `/etc/gaiko2/tdxgeth.env` and fill only operator-specific values.
4. Run `systemctl daemon-reload`.
5. Run `/usr/local/bin/runtime-init`.
6. Start `tdxs`, `taiko-geth`, `taiko-client`, and `gaiko2-tdxgeth`.
7. Run `tdx/scripts/smoke.sh` from outside or equivalent curl checks.

This SSH path proves service compatibility only. It is not a production trust
boundary because an operator can mutate the runtime after boot.
