-- +goose Up
CREATE TABLE test_workers (
    test_id TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (test_id, worker_id),

    FOREIGN KEY (test_id)
        REFERENCES tests(id)
        ON DELETE CASCADE,

    FOREIGN KEY (worker_id)
        REFERENCES workers(id)
        ON DELETE CASCADE
);

CREATE INDEX idx_test_workers_worker_id
ON test_workers(worker_id);

-- +goose Down
DROP INDEX IF EXISTS idx_test_workers_worker_id;
DROP TABLE IF EXISTS test_workers;
