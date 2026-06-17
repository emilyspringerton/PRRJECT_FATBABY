CREATE TABLE IF NOT EXISTS market_data_daily (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ticker          VARCHAR(20)     NOT NULL,
    trade_date      DATE            NOT NULL,
    open            DECIMAL(14,4)   NOT NULL DEFAULT 0,
    high            DECIMAL(14,4)   NOT NULL DEFAULT 0,
    low             DECIMAL(14,4)   NOT NULL DEFAULT 0,
    close           DECIMAL(14,4)   NOT NULL DEFAULT 0,
    adj_close       DECIMAL(14,4)   NOT NULL DEFAULT 0,
    volume          BIGINT UNSIGNED NOT NULL DEFAULT 0,
    ingested_at     TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    eventstore_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uq_ticker_date (ticker, trade_date),
    UNIQUE KEY uq_seq (eventstore_seq),
    INDEX idx_ticker_date  (ticker, trade_date DESC),
    INDEX idx_trade_date   (trade_date DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Daily OHLCV bars sourced from Yahoo Finance via market-data-watcher';
