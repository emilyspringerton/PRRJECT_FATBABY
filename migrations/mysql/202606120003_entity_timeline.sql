CREATE TABLE IF NOT EXISTS entity_timeline (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    entity_name     VARCHAR(255)    NOT NULL COMMENT 'director/auditor/activist/company name',
    entity_type     VARCHAR(32)     NOT NULL COMMENT 'director|auditor|activist|company',
    event_type      VARCHAR(64)     NOT NULL COMMENT 'appointed|resigned|changed|flagged|...',
    event_date      DATE            NOT NULL,
    role            VARCHAR(128)    NOT NULL DEFAULT '' COMMENT 'CEO|CFO|Board Member|Auditor|...',
    description     TEXT            NULL COMMENT 'human-readable event summary for Ask Emily',
    source_filing   VARCHAR(128)    NOT NULL DEFAULT '' COMMENT 'EDGAR accession number',
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_seq (eventstore_seq),
    INDEX idx_ticker_date (ticker, event_date DESC),
    INDEX idx_ticker_entity (ticker, entity_name),
    INDEX idx_entity_type (entity_type, event_type),
    INDEX idx_event_date (event_date DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
