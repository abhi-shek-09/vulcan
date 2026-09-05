package models

import "time"

type TestWorker struct {
	TestID      string
	WorkerID    string
	Status      string
	AssignedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}
