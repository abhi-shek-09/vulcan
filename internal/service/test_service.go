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

// Data Transfer Object (DTO)
// if not for dto, func (s *TestService) Create(ctx context.Context, name string)
// not Secure = they can send random requests to creat, sys will break
// not scalable = more params? your func call will change
type CreateTestRequest struct {
	Name string `json:"name"`
}

type TestService struct {
	repo repository.TestRepository // avoiding dependency injection
}

func NewTestService(repo repository.TestRepository) *TestService {
	return &TestService{
		repo: repo,
	}
}

func (s *TestService) CreateTest(ctx context.Context, req CreateTestRequest) (*models.Test, error) {

	req.Name = strings.TrimSpace(req.Name)

	if err := validation.Required("name", req.Name); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	test := &models.Test{
		ID:        ulid.MustNew(ulid.Timestamp(now), rand.Reader).String(),
		Name:      req.Name,
		Status:    models.StatusCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateTest(ctx, test); err != nil {
		return nil, err
	}

	return test, nil
}

func (s *TestService) GetTests(ctx context.Context) ([]models.Test, error) {
	return s.repo.GetTests(ctx)
}

func (s *TestService) GetTestByID(ctx context.Context, id string) (*models.Test, error) {
	return s.repo.GetTestByID(ctx, id)
}

func (s *TestService) StopTest(ctx context.Context, id string) error {
	test, err := s.repo.GetTestByID(ctx, id)
	if err != nil {
		return err
	}

	if test.Status == models.StatusStopped {
		return nil
	}

	return s.repo.UpdateStatus(
		ctx,
		id,
		models.StatusStopped,
	)
}
