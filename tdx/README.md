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

`mkosi.conf` is a profile skeleton. A production image build pipeline still
needs to provide:

- pinned binary build steps or prebuilt binary inputs
- user/group creation for `tdx`, `tdxs`, `taiko-geth`, `taiko-client`, and
  `gaiko2`
- a TDX-compatible kernel, initramfs, firmware, and platform profile
- persistent disk setup mounted at `/persistent`

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
