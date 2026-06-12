CREATE TABLE IF NOT EXISTS eps_results (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    period          VARCHAR(16)     NOT NULL COMMENT 'e.g. 2026Q1 (fiscal quarter)',
    eps_actual      DECIMAL(10,4)   NULL COMMENT 'reported EPS',
    eps_estimate    DECIMAL(10,4)   NULL COMMENT 'consensus estimate at time of report',
    surprise_pct    DECIMAL(8,4)    NULL COMMENT '(actual-estimate)/|estimate| * 100',
    revenue_actual  DECIMAL(18,2)   NULL COMMENT 'total revenue in USD',
    report_date     DATE            NOT NULL,
    filing_id       VARCHAR(128)    NOT NULL DEFAULT '',
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ticker_period (ticker, period),
    INDEX idx_ticker_date (ticker, report_date DESC),
    INDEX idx_report_date (report_date DESC),
    INDEX idx_surprise (surprise_pct)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
