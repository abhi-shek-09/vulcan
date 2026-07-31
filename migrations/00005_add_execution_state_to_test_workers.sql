-- +goose Up

ALTER TABLE test_workers
ADD COLUMN status TEXT NOT NULL DEFAULT 'RESERVED';

ALTER TABLE test_workers
ADD COLUMN started_at TIMESTAMPTZ;

ALTER TABLE test_workers
ADD COLUMN completed_at TIMESTAMPTZ;

CREATE INDEX idx_test_workers_status
ON test_workers(status);

-- +goose Down

DROP INDEX IF EXISTS idx_test_workers_status;

ALTER TABLE test_workers
DROP COLUMN IF EXISTS completed_at;

ALTER TABLE test_workers
DROP COLUMN IF EXISTS started_at;

ALTER TABLE test_workers
DROP COLUMN IF EXISTS status;