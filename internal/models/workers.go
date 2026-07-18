package models

import (
    "time"
)

type WorkerStatus string

const (
    WorkerStatusIdle        WorkerStatus = "IDLE"
    WorkerStatusReserved    WorkerStatus = "RESERVED"
    WorkerStatusRunning     WorkerStatus = "RUNNING"
    WorkerStatusDraining    WorkerStatus = "DRAINING"
    WorkerStatusOffline     WorkerStatus = "OFFLINE"
)

type Worker struct {
    ID              string       `json:"id"`
    Hostname        string       `json:"hostname"`
    Version         string       `json:"version"`
    Status          WorkerStatus `json:"status"`
    CPUCount        int          `json:"cpu_count"`
    MemoryMB        int64        `json:"memory_mb"`
    RegisteredAt    time.Time    `json:"registered_at"`
    LastHeartbeat   time.Time    `json:"last_heartbeat"`
    UpdatedAt       time.Time    `json:"updated_at"`
}