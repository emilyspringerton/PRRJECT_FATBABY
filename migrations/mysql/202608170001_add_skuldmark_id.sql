-- S175-02: add skuldmark_id to governance_signals and entity_timeline.
-- SKULDMARK-25 is the real instrument identifier minted at intake (S175-01,
-- secwatch/prwatch) and now carried through signal_generated events via
-- signal.RawMetadata["skuldmark_id"] (internal/processor/worker.go).
-- NULL for rows projected before this migration and for filings whose
-- exchange couldn't be resolved to a SKULDMARK mint at intake time --
-- never backfilled with a guess; projector replay will fill in what it can.

ALTER TABLE governance_signals
    ADD COLUMN IF NOT EXISTS skuldmark_id VARCHAR(25) NULL
        COMMENT 'SKULDMARK-25 instrument identifier; NULL when unminted at intake'
        AFTER entity_name;

ALTER TABLE governance_signals
    ADD INDEX IF NOT EXISTS idx_skuldmark_id (skuldmark_id);

ALTER TABLE entity_timeline
    ADD COLUMN IF NOT EXISTS skuldmark_id VARCHAR(25) NULL
        COMMENT 'SKULDMARK-25 instrument identifier; NULL when unminted at intake'
        AFTER entity_name;

ALTER TABLE entity_timeline
    ADD INDEX IF NOT EXISTS idx_skuldmark_id (skuldmark_id);
