-- Users, as versioned documents like dashboards (body holds the bcrypt hash).
CREATE TABLE IF NOT EXISTS users
(
    id         String,
    body       String,
    updated_at DateTime64(9),
    is_deleted UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree(updated_at, is_deleted)
ORDER BY id;

-- Shared secrets such as the session signing key. Every instance reads the
-- oldest value per name, so concurrent first starts agree on one key.
CREATE TABLE IF NOT EXISTS secrets
(
    name       String,
    value      String,
    created_at DateTime64(9)
)
ENGINE = MergeTree
ORDER BY name;
