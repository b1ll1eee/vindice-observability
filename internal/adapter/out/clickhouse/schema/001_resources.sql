-- Resource attributes (host, pod, version...) stored once and referenced by
-- resource_id from logs, spans and metric series.
CREATE TABLE IF NOT EXISTS resources
(
    resource_id  UInt64,
    service_name LowCardinality(String),
    attributes   Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    last_seen    DateTime
)
ENGINE = ReplacingMergeTree(last_seen)
ORDER BY resource_id
TTL {{.DimensionTTL}};
