package models

import (
	"time"
	"vulcan/internal/constants"
)

type TestStatus string

const (
	StatusCreated   TestStatus = constants.StatusCreated
	StatusStarting  TestStatus = constants.StatusStarting
	StatusRunning   TestStatus = constants.StatusRunning
	StatusStopping  TestStatus = constants.StatusStopping
	StatusStopped   TestStatus = constants.StatusStopped
	StatusCompleted TestStatus = constants.StatusCompleted
	StatusFailed    TestStatus = constants.StatusFailed
)

type Test struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      TestStatus `json:"status"`
	WorkerCount int        `json:"worker_count"`
	TargetURL   string     `json:"target_url"`
	Method      string     `json:"method"`
	DurationSec int        `json:"duration_sec"`
	RPS         int        `json:"rps"`
	Concurrency int        `json:"concurrency"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
