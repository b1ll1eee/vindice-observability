-- Logs. Sorting by service and severity keeps similar lines together, which
-- compresses much better. Retention depends on severity (see LogsTTL).
CREATE TABLE IF NOT EXISTS logs
(
    timestamp       DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    service_name    LowCardinality(String),
    severity_number UInt8,
    severity_text   LowCardinality(String),
    trace_id        FixedString(16) CODEC(ZSTD(1)),
    span_id         FixedString(8) CODEC(ZSTD(1)),
    resource_id     UInt64 CODEC(ZSTD(1)),
    scope_name      LowCardinality(String),
    body            String CODEC(ZSTD(3)),
    attributes      Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_body lower(body) TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (service_name, severity_number, timestamp)
TTL {{.LogsTTL}}
{{.ColdSettings}};

-- Log volume per service, severity and minute, for dashboards and alerts.
CREATE TABLE IF NOT EXISTS log_counts_1m
(
    ts              DateTime CODEC(DoubleDelta, ZSTD(1)),
    service_name    LowCardinality(String),
    severity_number UInt8,
    records         SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (service_name, severity_number, ts)
TTL {{.Rollup1mTTL}};

CREATE MATERIALIZED VIEW IF NOT EXISTS log_counts_1m_mv TO log_counts_1m AS
SELECT
    toStartOfMinute(toDateTime(timestamp)) AS ts,
    service_name,
    severity_number,
    count() AS records
FROM logs
GROUP BY ts, service_name, severity_number;
