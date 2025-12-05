# duties-indexer

## Overview

`duties-indexer` is a small monitoring service that connects to an Ethereum consensus (beacon) node and checks whether a configured set of validators actually performed their **proposer** and **attester** duties once an epoch is finalized.

For every new finalized epoch, the service:

- Fetches **proposer duties** and verifies that scheduled blocks were proposed at the correct slots.
- Fetches **attester duties** and checks whether corresponding attestations were included on-chain, taking into account:
  - Duty slot vs inclusion slot (attestations can be included up to 32 slots later).
  - Full committee layouts per slot (from the beacon node committees endpoint).
  - Correct use of `CommitteeBits` and `AggregationBits` to detect whether a validator participated.

Results are logged as successful or missed duties per validator, which can be compared against external dashboards or explorers.

## How it works (high level)

- The main loop (`DutiesChecker.run`) periodically polls the beacon node for the **latest finalized epoch**.
- For each new finalized epoch:
  - **Proposer checks**
    - Get proposer duties for the tracked validator indices.
    - For each duty, check if a block exists at that duty slot.
  - **Attester checks**
    - Get attester duties in batch for the tracked validators.
    - Collect all unique duty slots.
    - For each duty slot, call the beacon node once to get the full **committee size map**.
    - Preload attestations from blocks in the range `[minDutySlot+1 .. maxDutySlot+32]`.
    - For each duty, scan those attestations and:
      - Match on `DataSlot == duty.Slot` and the duty's `CommitteeIndex` via `CommitteeBits`.
      - Compute the validator's bit position using the committee sizes and `ValidatorCommitteeIdx`.
      - Check if the corresponding bit is set in `AggregationBits`.
    - Log ✅ when a matching attestation is found, otherwise log ❌ with duty details.

This design reduces repeated beacon-node calls (one committees call per duty slot; one attestations sweep per slot range) while keeping attestation detection correct in post-Electra networks.

## Running the service

### Prerequisites

- A running **beacon node** exposing the standard HTTP API, e.g. Prysm, Lighthouse, Teku, Nimbus.
- Go (1.21+ recommended) if running natively, or Docker if using containers.
- Environment/config entries for at least:
  - Beacon node URL (e.g. `http://localhost:5052`).
  - Validator identifiers to track (indices or pubkeys, depending on your config loader).
  - Poll interval (how often to check for new finalized epochs).

Check `internal/config/config_loader.go` for the exact env var names.

### Run with Docker

```bash
# From the repo root
# Example (adjust env vars to your setup)
BEACON_NODE_URL=http://your-beacon-node:5052 \
VALIDATOR_INDICES=1234,5678 \
POLL_INTERVAL=30s \
docker compose up --build
```

### Run with Go

```bash
# From the repo root
export BEACON_NODE_URL=http://your-beacon-node:5052
export VALIDATOR_INDICES=1234,5678
export POLL_INTERVAL=30s

go run ./cmd
```

Or build:

```bash
go build -o duties-indexer ./cmd
./duties-indexer
```