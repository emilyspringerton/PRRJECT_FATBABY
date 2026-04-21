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

## Project layout

- `eventstore/types.go`: core event/record types and `EventStore` interface.
- `eventstore/file_store.go`: file-backed implementation.
- `cmd/eventstore-demo/main.go`: simple demo program.

## License

This project is licensed under the terms of the MIT License. See [LICENSE](LICENSE).
