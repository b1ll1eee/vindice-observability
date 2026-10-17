-- Ingest API keys as versioned documents (body holds the SHA-256, never the key).
CREATE TABLE IF NOT EXISTS api_keys
(
    id         String,
    body       String,
    updated_at DateTime64(9),
    is_deleted UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree(updated_at, is_deleted)
ORDER BY id;

-- Last use per key, appended separately so recording use never rewrites
-- (and could never un-revoke) the key document.
CREATE TABLE IF NOT EXISTS api_key_use
(
    key_id  String,
    used_at DateTime
)
ENGINE = ReplacingMergeTree(used_at)
ORDER BY key_id
TTL used_at + INTERVAL 400 DAY;
