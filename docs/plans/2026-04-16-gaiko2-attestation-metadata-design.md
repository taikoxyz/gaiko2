# gaiko2 Attestation Metadata Design

## Goal

Persist the SGX image identity used by `gaiko2` tee releases as a stable release
artifact so registration and proposal tooling can read it without re-entering a
container or re-parsing a quote ad hoc.

## Design

`gaiko2` tee images should contain an attestation metadata file generated at
image build time. The file should include at least:

- `unique_id` (EGo unique id / MRENCLAVE-style measurement)
- `signer_id`
- `product_id`
- `security_version`

The file lives inside the tee image and is treated as immutable image metadata.

During `bootstrap`, `gaiko2` should copy that metadata into the mounted config
directory as:

- `attestation.gaiko2.json`

This makes it part of the release directory beside:

- `bootstrap.gaiko2.json`
- `registered.gaiko2.json`

## CLI

Add:

- `gaiko2 metadata`

This command prints the embedded image metadata file. It is primarily a
debugging and inspection tool; normal operator workflows should read the copied
release artifact instead.

## Deploy Integration

The deploy script should treat:

- `deploy/<fork>/<release>/config/attestation.gaiko2.json`

as a first-class artifact and pass it to the register hook via:

- `GAIKO2_ATTESTATION_JSON`

## Rationale

This avoids three weak patterns:

- depending on Docker tags as a trust anchor
- requiring operators to exec into a container to recover MRENCLAVE
- coupling registration tooling to quote parsing when a stable release artifact
  is enough for most control-plane flows

The quote remains the cryptographic trust anchor. The metadata file is an
operationally friendly representation of the image identity.
