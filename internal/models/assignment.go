package models

import "time"

type AssignmentStatus string

const (
	AssignmentStatusReserved  AssignmentStatus = "RESERVED"
	AssignmentStatusRunning   AssignmentStatus = "RUNNING"
	AssignmentStatusCompleted AssignmentStatus = "COMPLETED"
)

type Assignment struct {
	TestID      string
	WorkerID    string

	Status      AssignmentStatus

	AssignedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}
