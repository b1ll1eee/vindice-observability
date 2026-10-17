-- Dashboards and alert rules: small, versioned documents. ReplacingMergeTree
-- keeps the newest version per id; a delete writes a version with is_deleted = 1.
CREATE TABLE IF NOT EXISTS dashboards
(
    id         String,
    body       String CODEC(ZSTD(3)),
    updated_at DateTime64(9),
    is_deleted UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree(updated_at, is_deleted)
ORDER BY id;

CREATE TABLE IF NOT EXISTS alert_rules
(
    id         String,
    body       String CODEC(ZSTD(3)),
    updated_at DateTime64(9),
    is_deleted UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree(updated_at, is_deleted)
ORDER BY id;

-- Evaluator state per rule, so a restart neither re-sends nor forgets alerts.
CREATE TABLE IF NOT EXISTS alert_status
(
    rule_id    String,
    body       String,
    updated_at DateTime64(9)
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY rule_id
TTL toDateTime(updated_at) + INTERVAL 30 DAY;

CREATE TABLE IF NOT EXISTS alert_events
(
    rule_id   String,
    rule_name String,
    ts        DateTime64(3),
    state     LowCardinality(String),
    value     Float64,
    message   String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, rule_id)
TTL toDateTime(ts) + INTERVAL 90 DAY;
