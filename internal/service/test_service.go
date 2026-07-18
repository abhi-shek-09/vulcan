package service

import (
	"context"
	"crypto/rand"
	"strconv"
	"strings"
	"time"
	"github.com/oklog/ulid/v2"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"
	"vulcan/internal/repository"
	"vulcan/internal/scheduler"
	"vulcan/internal/validation"
)

type CreateTestRequest struct {
	Name        string `json:"name" validate:"required"`
	WorkerCount int    `json:"worker_count" validate:"required,min=1"`
}

type TestService struct {
	testRepo      repository.TestRepository
    workerRepo  repository.WorkerRepository
	scheduler scheduler.Scheduler
}

func NewTestService(
	testRepo repository.TestRepository,
	scheduler scheduler.Scheduler,
	workerRepo repository.WorkerRepository,
) *TestService {
	return &TestService{
		testRepo:      testRepo,
		scheduler: scheduler,
		workerRepo: workerRepo,
	}
}

func (s *TestService) CreateTest(
	ctx context.Context,
	req CreateTestRequest,
) (*models.Test, error) {

	req.Name = strings.TrimSpace(req.Name)

	if err := validation.Required("name", req.Name); err != nil {
		return nil, err
	}

	if req.WorkerCount < 1 {
		return nil, validation.Required("worker_count", strconv.Itoa(req.WorkerCount))
	}

	now := time.Now().UTC()

	test := &models.Test{
		ID:          ulid.MustNew(ulid.Timestamp(now), rand.Reader).String(),
		Name:        req.Name,
		WorkerCount: req.WorkerCount,
		Status:      models.StatusCreated,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.testRepo.CreateTest(ctx, test); err != nil {
		return nil, err
	}

	return test, nil
}

func (s *TestService) GetTests(ctx context.Context,) ([]models.Test, error) {
	return s.testRepo.GetTests(ctx)
}

func (s *TestService) GetTestByID(ctx context.Context,id string,) (*models.Test, error) {
	return s.testRepo.GetTestByID(ctx, id)
}

func (s *TestService) StopTest(ctx context.Context,id string,) error {

	test, err := s.testRepo.GetTestByID(ctx, id)
	if err != nil {
		return err
	}

	if test.Status == models.StatusStopped {
		return nil
	}

	if err := s.workerRepo.ReleaseWorkersForTest(ctx, id); err != nil {
		return err
	}

	if err := s.testRepo.UpdateStatus(
		ctx,
		id,
		models.StatusStopped,
	); err != nil {
		return err
	}

	return nil
}

func (s *TestService) StartTest(ctx context.Context,testID string,) ([]models.Worker, error) {

	test, err := s.testRepo.GetTestByID(ctx, testID)
	if err != nil {
		return nil, err
	}

	// Prevent starting the same test twice
	if test.Status != models.StatusCreated {
		return nil, apierrors.ErrInvalidTestState
	}

	workers, err := s.scheduler.AllocateWorkers(
		ctx,
		test.ID,
		test.WorkerCount,
	)
	if err != nil {
		return nil, err
	}

	if err := s.testRepo.UpdateStatus(
		ctx,
		test.ID,
		models.StatusRunning,
	); err != nil {
		return nil, err
	}

	return workers, nil
}