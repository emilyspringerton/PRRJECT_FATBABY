CREATE TABLE IF NOT EXISTS projector_cursors (
    projector_name  VARCHAR(64)     NOT NULL COMMENT 'e.g. governance_signals|eps_results|entity_timeline',
    last_seq        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'last processed eventstore sequence number',
    updated_at      TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (projector_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
