-- Sampled spans. Hot HTTP attributes are promoted to their own columns.
CREATE TABLE IF NOT EXISTS spans
(
    timestamp         DateTime64(9) CODEC(DoubleDelta, ZSTD(1)),
    trace_id          FixedString(16) CODEC(ZSTD(1)),
    span_id           FixedString(8) CODEC(ZSTD(1)),
    parent_span_id    FixedString(8) CODEC(ZSTD(1)),
    service_name      LowCardinality(String),
    span_name         LowCardinality(String),
    span_kind         Enum8('Unspecified' = 0, 'Internal' = 1, 'Server' = 2, 'Client' = 3, 'Producer' = 4, 'Consumer' = 5),
    status_code       Enum8('Unset' = 0, 'Ok' = 1, 'Error' = 2),
    status_message    String CODEC(ZSTD(1)),
    duration_ns       UInt64 CODEC(T64, ZSTD(1)),
    resource_id       UInt64 CODEC(ZSTD(1)),
    scope_name        LowCardinality(String),
    http_method       LowCardinality(String),
    http_route        LowCardinality(String),
    http_status_code  UInt16,
    attributes        Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    events_timestamp  Array(DateTime64(9)) CODEC(ZSTD(1)),
    events_name       Array(LowCardinality(String)),
    events_attributes Array(Map(LowCardinality(String), String)) CODEC(ZSTD(3)),
    INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_duration duration_ns TYPE minmax GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (service_name, span_name, timestamp)
TTL {{.SpansTTL}}
{{.ColdSettings}};

-- trace_id -> time window, so opening a trace by ID scans one day, not all.
CREATE TABLE IF NOT EXISTS trace_index
(
    trace_id FixedString(16),
    start_ts SimpleAggregateFunction(min, DateTime64(9)),
    end_ts   SimpleAggregateFunction(max, DateTime64(9))
)
ENGINE = AggregatingMergeTree
ORDER BY trace_id
TTL {{.TraceIndexTTL}};

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_index_mv TO trace_index AS
SELECT trace_id, min(timestamp) AS start_ts, max(timestamp) AS end_ts
FROM spans
GROUP BY trace_id;

-- Entry spans before sampling. Null engine: nothing is stored, rows only
-- feed red_1m so request rate, errors and latency stay exact.
CREATE TABLE IF NOT EXISTS span_stats
(
    timestamp    DateTime64(9),
    service_name LowCardinality(String),
    span_name    LowCardinality(String),
    status_code  Enum8('Unset' = 0, 'Ok' = 1, 'Error' = 2),
    duration_ns  UInt64
)
ENGINE = Null;

CREATE TABLE IF NOT EXISTS red_1m
(
    ts           DateTime CODEC(DoubleDelta, ZSTD(1)),
    service_name LowCardinality(String),
    span_name    LowCardinality(String),
    requests     SimpleAggregateFunction(sum, UInt64),
    errors       SimpleAggregateFunction(sum, UInt64),
    duration_sum SimpleAggregateFunction(sum, UInt64),
    duration_q   AggregateFunction(quantilesTDigest(0.5, 0.95, 0.99), UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (service_name, span_name, ts)
TTL {{.Rollup1mTTL}};

CREATE MATERIALIZED VIEW IF NOT EXISTS red_1m_mv TO red_1m AS
SELECT
    toStartOfMinute(toDateTime(timestamp)) AS ts,
    service_name,
    span_name,
    count() AS requests,
    countIf(status_code = 'Error') AS errors,
    sum(duration_ns) AS duration_sum,
    quantilesTDigestState(0.5, 0.95, 0.99)(duration_ns) AS duration_q
FROM span_stats
GROUP BY ts, service_name, span_name;
