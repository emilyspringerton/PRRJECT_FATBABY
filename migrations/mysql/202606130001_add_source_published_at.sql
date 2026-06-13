-- Add source_published_at to governance_signals.
-- This is the original source document publish date (SEC filing date or PR publish date),
-- distinct from ingested_at (pipeline ingest timestamp).
-- Populated by the projector from signal.RawMetadata["source_published_at"].
-- NULL for signals projected before this migration; projector replay will backfill.

ALTER TABLE governance_signals
    ADD COLUMN IF NOT EXISTS source_published_at DATE NULL
        COMMENT 'original source document date; SEC filing_date or PR published_at'
        AFTER filing_date;

ALTER TABLE governance_signals
    ADD INDEX IF NOT EXISTS idx_ticker_pub_date (ticker, source_published_at DESC);
