# Gaiko2 Provider Logging Design

## Problem

`gaiko2-sgxgeth` currently emits only minimal startup output and almost no application-level request logs. In Docker this makes it hard to confirm:

- what configuration the provider started with
- when it is listening
- whether proposal and aggregate requests succeed or fail

## Goal

Add lightweight provider logs without adding hot-path noise or changing protocol behavior.

## Approach

Use the standard library logging surface already appropriate for this small Go binary:

1. print a startup summary from `cmd/gaiko2`
2. keep the existing listening line
3. emit one success or failure line per proposal request
4. emit one success or failure line per aggregate request

The startup summary should stay small and safe:

- mode
- tee_type
- fork
- instance_id
- config_dir
- secret_dir
- listen

No keys, quotes, or full bootstrap payloads should be logged.

## Request Logging

Proposal request logs should include:

- schema
- chain_id
- block_count
- success or failure

Aggregate request logs should include:

- schema
- proof_count
- success or failure

There should be no separate request-received log line.

## Testing

Add focused tests for:

- startup summary output in `cmd/gaiko2`
- success logging for proposal requests
- failure logging for aggregate requests
