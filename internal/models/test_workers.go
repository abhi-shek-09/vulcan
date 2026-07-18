package models

import "time"

type TestWorker struct {
    TestID     string
    WorkerID   string
    AssignedAt time.Time
}