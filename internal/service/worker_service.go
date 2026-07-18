package service

import (
	"context"
	"crypto/rand"
	"strings"
	"time"
	"vulcan/internal/models"
	"vulcan/internal/repository"
	"vulcan/internal/validation"
	"github.com/oklog/ulid/v2"
)

type RegisterWorkerRequest struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	CPUCount int    `json:"cpu_count"`
	MemoryMB int64  `json:"memory_mb"`
}

type HeartbeatRequest struct {
    Status models.WorkerStatus `json:"status"`
}

type WorkerService struct {
	repo repository.WorkerRepository
}

func NewWorkerService(repo repository.WorkerRepository) *WorkerService {
	return &WorkerService{
		repo: repo,
	}
}

func (s *WorkerService) RegisterWorker(
	ctx context.Context,
	req RegisterWorkerRequest,
) (*models.Worker, error) {

	req.Hostname = strings.TrimSpace(req.Hostname)
	req.Version = strings.TrimSpace(req.Version)

	if err := validation.Required("hostname", req.Hostname); err != nil {
		return nil, err
	}

	if err := validation.Required("version", req.Version); err != nil {
		return nil, err
	}

	if req.CPUCount <= 0 {
		return nil, validation.Invalid("cpu_count", "must be greater than 0")
	}

	if req.MemoryMB <= 0 {
		return nil, validation.Invalid("memory_mb", "must be greater than 0")
	}

	now := time.Now().UTC()

	worker := &models.Worker{
		ID:            ulid.MustNew(ulid.Timestamp(now),rand.Reader,).String(), // Generates a cryptographically secure random UUIDv4
		Hostname:      req.Hostname,
		Version:       req.Version,
		Status:        models.WorkerStatusIdle,
		CPUCount:      req.CPUCount,
		MemoryMB:      req.MemoryMB,
		RegisteredAt:  now,
		LastHeartbeat: now,
		UpdatedAt:     now,
	}

	// Aligned with the interface method name (.Create)
	if err := s.repo.CreateWorker(ctx, worker); err != nil {
		return nil, err
	}

	return worker, nil
}

func (s *WorkerService) GetWorkers(ctx context.Context) ([]models.Worker, error) {
	// Aligned with the interface method name (.List)
	return s.repo.GetWorkers(ctx)
}

func (s *WorkerService) GetWorkerByID(
	ctx context.Context,
	id string,
) (*models.Worker, error) {
	// Aligned with the interface method name (.GetByID)
	return s.repo.GetWorkerByID(ctx, id)
}

func (s *WorkerService) Heartbeat(ctx context.Context, id string, req HeartbeatRequest) error {
	
	switch req.Status {
		case
		// do not allow offline or registering
		// only control plane is allowed to do that
		models.WorkerStatusIdle,
		models.WorkerStatusReserved,
		models.WorkerStatusRunning,
		models.WorkerStatusDraining:
	default:
		return validation.Invalid("status", "invalid worker status")
	}

	return s.repo.UpdateHeartbeat(
		ctx,
		id,
		req.Status,
	)
}