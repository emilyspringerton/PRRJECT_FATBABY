# prrject-fatbaby

`prrject-fatbaby` is a small Go event store library with a file-backed implementation.

## Features

- Event model with validation (`ID`, `Type`, and `Data` are required).
- Append-only record storage with monotonically increasing global sequence numbers.
- Journal files split by UTC date.
- Latest sequence persisted in a state file for fast recovery.
- Read records from a given sequence with a configurable limit.

## Installation

```bash
go get github.com/example/prrject-fatbaby
```

## Quick start

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

func main() {
	store, err := eventstore.NewFileStore("./data")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	payload, _ := json.Marshal(map[string]any{"order_id": "A123", "amount": 4200})
	records, err := store.Append(context.Background(), eventstore.Event{
		ID:         "evt-1",
		Type:       "order.created",
		OccurredAt: time.Now().UTC(),
		Data:       payload,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("appended sequence: %d\n", records[0].Sequence)
}
```

## Running tests

```bash
go test ./...
```

## SEC watchlist discovery (phase 1)

This repo now includes a conservative SEC submissions discovery tool:

- Watchlist config: `config/watchlist.json`
- Command: `cmd/secwatch/main.go`
- Discovery package: `secwatch/`

Run a safe dry-run poll:

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch \
  -dry-run
```

Run real mode (persists `filing_discovered` events in the file-backed event store):

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch
```

Run continuously with a conservative polling cadence:

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch \
  -poll-interval 5m
```

## Track A — modest historical backfill

Not “download the SEC.”
Not “mirror EDGAR.”

A controlled rolling backfill for your watched issuers/forms.

This gives you:

- temporal depth,
- repeated quarterly structure,
- amendments,
- issuer-specific drift,
- parser regression coverage,
- and eventually training/eval material.

### Backfill philosophy

Use **shallow breadth first, then selective depth**:

1. Pull a few years for each watched issuer.
2. Cover a focused form set (`10-K`, `10-Q`, `8-K`, plus `10-K/A` / `10-Q/A` when present).
3. Keep strict per-issuer and per-run caps so ingestion stays deterministic and cheap.
4. Persist a local manifest with enough metadata to replay fixture generation.

### Practical starting point

- Time window: last **2–3 years** per issuer.
- Forms: start with `10-K`, `10-Q`, `8-K` (+ amendments).
- Per run cap: ~25 filings max total (e.g., 5–10 per issuer).
- Corpus policy: only keep primary filing HTML plus JSON metadata/index.

### Recommended rollout phases

1. **Phase A1 (breadth):** Populate all watched issuers with at least one annual + one quarterly filing.
2. **Phase A2 (quarterly depth):** Fill missing quarter chains for each issuer.
3. **Phase A3 (amendments):** Explicitly add amendment forms and verify supersession handling.
4. **Phase A4 (drift):** Extend selected issuers further back for parser regression and historical edge cases.

### Why this works for this repo

The repository already keeps fixture snapshots by issuer under `fixtures/` alongside `manifest.json` files. A rolling backfill strategy keeps fixture volume manageable while still increasing parser and discovery coverage over time.

## Project layout

- `eventstore/types.go`: core event/record types and `EventStore` interface.
- `eventstore/file_store.go`: file-backed implementation.
- `cmd/eventstore-demo/main.go`: simple demo program.

## License

This project is licensed under the terms of the MIT License. See [LICENSE](LICENSE).
