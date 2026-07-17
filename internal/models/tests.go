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
)

type Test struct {
	ID        string
	Name      string
	Status    TestStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
