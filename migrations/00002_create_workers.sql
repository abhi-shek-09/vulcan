-- +goose Up
CREATE TABLE workers (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    version TEXT NOT NULL,
    status TEXT NOT NULL,
    cpu_count INTEGER NOT NULL,
    memory_mb BIGINT NOT NULL, -- if we shift to bytes later, we wouldn't have to migrate again from int to bigint
    registered_at TIMESTAMPTZ NOT NULL,
    last_heartbeat TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_workers_status
ON workers(status);

CREATE INDEX idx_workers_last_heartbeat
ON workers(last_heartbeat);

-- +goose Down
DROP INDEX IF EXISTS idx_workers_status;
DROP INDEX IF EXISTS idx_workers_last_heartbeat;
DROP TABLE IF EXISTS workers;

