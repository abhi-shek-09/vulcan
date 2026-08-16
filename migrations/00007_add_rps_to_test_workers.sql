-- +goose Up

ALTER TABLE test_workers
ADD COLUMN rps INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE test_workers
DROP COLUMN IF EXISTS rps;
