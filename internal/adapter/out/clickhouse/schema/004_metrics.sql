-- One row per series (name + attributes + resource); samples only carry series_id.
CREATE TABLE IF NOT EXISTS metric_series
(
    series_id    UInt64,
    metric_name  LowCardinality(String),
    service_name LowCardinality(String),
    metric_type  Enum8('gauge' = 1, 'sum' = 2, 'histogram' = 3, 'summary' = 4),
    temporality  Enum8('unspecified' = 0, 'delta' = 1, 'cumulative' = 2),
    is_monotonic Bool,
    unit         LowCardinality(String),
    resource_id  UInt64,
    attributes   Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    last_seen    DateTime
)
ENGINE = ReplacingMergeTree(last_seen)
ORDER BY (metric_name, series_id)
TTL {{.DimensionTTL}};

CREATE TABLE IF NOT EXISTS metric_samples
(
    series_id UInt64,
    timestamp DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),
    value     Float64 CODEC(Gorilla, ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (series_id, timestamp)
TTL {{.SamplesTTL}}
{{.ColdSettings}};

-- Downsampled rollups: raw -> 1 minute -> 1 hour, each with its own retention.
CREATE TABLE IF NOT EXISTS metric_samples_1m
(
    series_id UInt64,
    ts        DateTime CODEC(DoubleDelta, ZSTD(1)),
    v_min     SimpleAggregateFunction(min, Float64),
    v_max     SimpleAggregateFunction(max, Float64),
    v_sum     SimpleAggregateFunction(sum, Float64),
    v_count   SimpleAggregateFunction(sum, UInt64),
    v_last    AggregateFunction(argMax, Float64, DateTime64(3))
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (series_id, ts)
TTL {{.Rollup1mTTL}};

CREATE MATERIALIZED VIEW IF NOT EXISTS metric_samples_1m_mv TO metric_samples_1m AS
SELECT
    series_id,
    toStartOfMinute(toDateTime(timestamp)) AS ts,
    min(value) AS v_min,
    max(value) AS v_max,
    sum(value) AS v_sum,
    count() AS v_count,
    argMaxState(value, timestamp) AS v_last
FROM metric_samples
GROUP BY series_id, ts;

CREATE TABLE IF NOT EXISTS metric_samples_1h
(
    series_id UInt64,
    ts        DateTime CODEC(DoubleDelta, ZSTD(1)),
    v_min     SimpleAggregateFunction(min, Float64),
    v_max     SimpleAggregateFunction(max, Float64),
    v_sum     SimpleAggregateFunction(sum, Float64),
    v_count   SimpleAggregateFunction(sum, UInt64),
    v_last    AggregateFunction(argMax, Float64, DateTime64(3))
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (series_id, ts)
TTL {{.Rollup1hTTL}};

CREATE MATERIALIZED VIEW IF NOT EXISTS metric_samples_1h_mv TO metric_samples_1h AS
SELECT
    series_id,
    toStartOfHour(toDateTime(timestamp)) AS ts,
    min(value) AS v_min,
    max(value) AS v_max,
    sum(value) AS v_sum,
    count() AS v_count,
    argMaxState(value, timestamp) AS v_last
FROM metric_samples
GROUP BY series_id, ts;
