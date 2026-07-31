-- Schema for the configs table.
--
-- Applied idempotently on service startup (see Postgres.Migrate). This keeps
-- local setup to a single command with no separate migration tool, at the
-- cost of not supporting incremental schema evolution — acceptable for the
-- scope of this assignment (see README "Known limitations").
CREATE TABLE IF NOT EXISTS configs (
    id         VARCHAR(255) PRIMARY KEY,
    host       VARCHAR(255) NOT NULL,
    port       INTEGER      NOT NULL CHECK (port > 0 AND port <= 65535),
    app_name   VARCHAR(255) NOT NULL,
    log_level  VARCHAR(50)  NOT NULL DEFAULT 'INFO',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Supports lookups/filtering by application name (not required by the
-- current API, but a realistic access pattern for a config service).
CREATE INDEX IF NOT EXISTS idx_configs_app_name ON configs (app_name);
