# Runbook — News Site End-to-End

Getting live SEC filing content rendering at `http://localhost:8082`.

---

## Overview of what needs to happen

Four things must have occurred before the news site shows any content, in this exact causal order:

1. **`secwatch`** polls SEC EDGAR and writes `filing_discovered` events into the event store.
2. **`processor`** reads those events, fetches the filing document from SEC, cleans it, and writes a `source_document_persisted` event into the same store.
3. **`newssite`** reads `source_document_persisted` events on every HTTP request and renders them as HTML.
4. You open a browser.

Steps 1 and 2 must complete at least one cycle before step 3 shows anything other than the empty-state page. Steps 1–3 can all run at the same time after the first discovery pass has already put events in the store.

---

## Prerequisites

- Go 1.22 or later installed (`go version` to confirm).
- Internet access to `data.sec.gov` — secwatch talks directly to SEC EDGAR.
- The repository cloned and you are at its root.

Verify everything compiles before running anything:

```bash
go build ./...
```

Fix any compile errors before proceeding.

---

## Step 1 — Create the data directory

The event store writes to a local directory. All three processes must point at the same path. Use `var/secwatch` to match the defaults.

```bash
mkdir -p var/secwatch
```

This only needs to be done once. If the directory already exists with data in it from a previous run, leave it — the store appends and the processor is idempotent (it checks for existing `source_document_persisted` events before re-fetching).

---

## Step 2 — Run a dry-run SEC discovery pass (optional but recommended)

Before writing anything to disk, do a dry-run to confirm SEC EDGAR is reachable and the watchlist parses correctly. This makes no network writes and touches no files in `var/secwatch`.

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch \
  -dry-run
```

You will see log lines like:

```
secwatch dry-run filing ticker=AAPL cik=0000320193 accession=... form=8-K
```

If you see those, EDGAR is reachable and the watchlist is loading. If you see connection errors, check your network. If you see `read watchlist` errors, check that `config/watchlist.json` exists.

The watchlist currently covers: AAPL, MSFT, NVDA, BLK, STT, GOOG, GOOGL, BRK.A, BRK.B, MSTR, PLTR, SCHW, BEN, BAC, JPM, WFC, LLY — 17 issuers across tech, finance, and pharma, tracking `8-K`, `10-Q`, and `10-K` forms.

---

## Step 3 — Run the real SEC discovery pass

This writes `filing_discovered` events to `var/secwatch`. Run it once to seed the store.

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch
```

Expected output — one block per issuer, then a summary:

```
2026/05/22 ... data directory var/secwatch
2026/05/22 ... secwatch discovered ticker=AAPL cik=... accession=... form=8-K
2026/05/22 ... secwatch discovered ticker=MSFT ...
...
```

Each `discovered` line means a `filing_discovered` event was appended to the store. `skipped` lines mean the filing was already in the store from a previous run — that is fine.

This pass hits SEC EDGAR at 2 requests/second (the default `-rate-rps`). With 17 issuers it takes roughly 30–60 seconds. Do not interrupt it.

When it exits cleanly (no `Fatalf`), at least some `filing_discovered` events are in the store and you can move on.

---

## Step 4 — Run the processor

The processor reads `filing_discovered` events, fetches the actual filing document from the URL in each event, cleans it, and writes a `source_document_persisted` event. This is the step that produces content the news site can display.

Open a new terminal tab and run:

```bash
go run ./cmd/processor \
  -store ./var/secwatch \
  -workers 4 \
  -poll-interval 15s
```

Leave this running. It does not exit — it polls continuously.

Expected output for a successful document:

```
2026/05/22 ... processor handle start identity=0000320193:... form=8-K doc=https://...
2026/05/22 ... processor fetch complete identity=0000320193:... cleaned_chars=42107
2026/05/22 ... processor source_document persisted identity=0000320193:... ticker=AAPL chars=42107
2026/05/22 ... processor handle complete identity=0000320193:...
```

The critical line is `source_document persisted`. That event is what the news site reads.

Expected output when a document was already persisted (idempotent replay):

```
2026/05/22 ... processor source_document already persisted identity=...
```

That is correct — the processor will not re-fetch or re-write.

**If you see `fetch failed`:** the filing URL returned an error from SEC. This happens occasionally — SEC sometimes 403s on older filings. The processor logs the failure, appends a `signal_failed` event, and moves on to the next filing. Other filings will still be processed.

**If you see `filing too large`:** the document exceeded 4 MB (the default `-max-doc-bytes`). It is skipped. This is uncommon for 8-Ks.

Wait until you see at least one `source_document persisted` line before opening the news site. With 4 workers and fast EDGAR responses, the first few documents usually appear within 30 seconds of the processor starting.

---

## Step 5 — Start the news site

Open another terminal tab:

```bash
go run ./cmd/newssite \
  -store ./var/secwatch \
  -addr :8082
```

Expected output:

```
2026/05/22 ... newssite listening addr=:8082 store=var/secwatch
```

The server is ready immediately — it reads from the store on every HTTP request, so there is no startup index to wait for.

---

## Step 6 — Open the news site

```
http://localhost:8082
```

You will see the list page with one article entry per `source_document_persisted` event, newest first. Each entry shows:

- The ticker (`AAPL`, `MSFT`, etc.) as a heading with a link to the detail page.
- A label: `SEC Filing` for 8-Ks, 10-Qs, 10-Ks. `Press Release` for press releases (when those flow through).
- The form type and the timestamp the document was persisted.
- The first ~800 characters of the cleaned filing text as a preview.
- A `Read full document →` link.

Click any ticker heading or `Read full document →` to go to the detail page at `/doc/{identity}`. The detail page shows the full cleaned plain-text of the filing, the source URL as a link, and a back-navigation link.

If you see the empty-state message instead of articles, the processor has not yet written any `source_document_persisted` events. Go back to the processor terminal and wait for the `source_document persisted` log line.

---

## Running continuously

For ongoing content — new filings as companies publish them — run all three processes together in separate terminals:

**Terminal 1 — Discovery (every 5 minutes):**

```bash
go run ./cmd/secwatch \
  -watchlist ./config/watchlist.json \
  -store ./var/secwatch \
  -poll-interval 5m
```

**Terminal 2 — Processor (always running):**

```bash
go run ./cmd/processor \
  -store ./var/secwatch \
  -workers 4 \
  -poll-interval 15s
```

**Terminal 3 — News site:**

```bash
go run ./cmd/newssite \
  -store ./var/secwatch \
  -addr :8082
```

New filings discovered by secwatch will be picked up by the processor within 15 seconds (one poll cycle), persisted as source documents, and visible on the next page load of the news site.

---

## Inspecting the event store directly

The store writes NDJSON files under `var/secwatch/events/`. Each file is named by UTC date. To see what events are in the store:

```bash
# Count all events
cat var/secwatch/events/*.ndjson | wc -l

# Show only source_document_persisted events (one per line, jq-formatted)
cat var/secwatch/events/*.ndjson | grep '"type":"source_document_persisted"' | jq .

# Show just the tickers and char counts from persisted source documents
cat var/secwatch/events/*.ndjson \
  | grep '"type":"source_document_persisted"' \
  | jq -r '.event.data | fromjson | "\(.ticker)\t\(.cleaned_char_count) chars\t\(.source_type)"'

# Show just filing_discovered events
cat var/secwatch/events/*.ndjson \
  | grep '"type":"filing_discovered"' \
  | jq -r '.event.data | fromjson | "\(.ticker)\t\(.form)\t\(.accession_number)"'

# Check the latest sequence number
cat var/secwatch/state/latest-sequence
```

If `source_document_persisted` events exist in the NDJSON files but the news site is still showing the empty state, the `-store` flag passed to `cmd/newssite` is pointing at a different directory than where the processor wrote its output. Confirm both use the same resolved path:

```bash
ls -la var/secwatch/events/
```

---

## Troubleshooting

**News site shows empty state after processor has been running for several minutes.**

Check that the processor log shows `source_document persisted` (not just `fetch complete`). If `persist_source.go` was not implemented yet, the event type will never appear. Confirm with:

```bash
cat var/secwatch/events/*.ndjson | grep -c '"source_document_persisted"'
```

If that prints `0`, the processor code changes from the previous Codex session have not been applied.

**secwatch exits immediately with no discovered filings.**

Run with `-dry-run` first and look at what forms are being seen. If SEC returns empty submissions for all issuers, the watchlist CIKs may need verification. AAPL CIK `320193` is a reliable canary — if it returns nothing, the problem is network access to `data.sec.gov`.

**Processor logs `fetch failed` for every filing.**

SEC EDGAR rate-limits aggressive crawlers. The default 2 RPS should be fine. If you ran multiple processor instances against the same store simultaneously, SEC may have temporarily throttled the IP. Wait a few minutes and restart with one instance.

**Port 8082 already in use.**

Change the port:

```bash
go run ./cmd/newssite -store ./var/secwatch -addr :8083
```

Then open `http://localhost:8083`.

**`go build ./...` fails with `cannot find package` for `internal/newssite`.**

The `cmd/newssite/main.go` and `internal/newssite/*.go` files need to exist on disk. If Codex generated them, ensure they were saved to the correct paths relative to the repository root before building.

---

## Sequence diagram

```text
secwatch              event store (var/secwatch)        processor             newssite        browser
   |                          |                              |                    |               |
   |--RunDiscovery----------->|                              |                    |               |
   |  (polls EDGAR, writes    |                              |                    |               |
   |   filing_discovered)     |                              |                    |               |
   |                          |<--ReadFrom(seq=1, 512)-------|                    |               |
   |                          |--[]Record(filing_discovered)->|                   |               |
   |                          |                              |--FetchAndCleanText->               |
   |                          |                              |  (GET filing from SEC.gov)         |
   |                          |                              |<-cleaned text-------|               |
   |                          |<--Append(source_document_persisted)----------------|               |
   |                          |<--Append(signal_generated)--------------------------|               |
   |                          |                              |                    |               |
   |                          |                              |            GET / ->|               |
   |                          |<--ReadFrom(seq=1, 512)---------------------------|               |
   |                          |--[]Record(source_document_persisted)------------>|               |
   |                          |                              |         HTML body->|               |
   |                          |                              |                    |--HTML-------->|
```
