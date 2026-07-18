-- +goose Up

ALTER TABLE tests
ADD COLUMN worker_count INTEGER NOT NULL DEFAULT 1;

-- +goose Down

ALTER TABLE tests
DROP COLUMN worker_count;