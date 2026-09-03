package autoscaler

import (
	"testing"

	"vulcan/internal/models"
)

func TestDesiredCapacity(t *testing.T) {
	tests := []models.Test{
		{
			ID:     "test-1",
			Status: models.StatusRunning,
			RPS:    100,
		},
		{
			ID:     "test-2",
			Status: models.StatusRunning,
			RPS:    150,
		},
	}

	got, err := DesiredCapacity(tests, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 100/50 = 2
	// 150/50 = 3
	// Total = 5
	if got != 5 {
		t.Fatalf("expected desired capacity 5, got %d", got)
	}
}

func TestDesiredCapacityIgnoresInactiveTests(t *testing.T) {
	tests := []models.Test{
		{
			ID:     "created",
			Status: models.StatusCreated,
			RPS:    1000,
		},
		{
			ID:     "running",
			Status: models.StatusRunning,
			RPS:    100,
		},
		{
			ID:     "completed",
			Status: models.StatusCompleted,
			RPS:    1000,
		},
		{
			ID:     "stopped",
			Status: models.StatusStopped,
			RPS:    1000,
		},
		{
			ID:     "failed",
			Status: models.StatusFailed,
			RPS:    1000,
		},
	}

	got, err := DesiredCapacity(tests, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != 2 {
		t.Fatalf("expected desired capacity 2, got %d", got)
	}
}

func TestDesiredCapacityIncludesStartingTests(t *testing.T) {
	tests := []models.Test{
		{
			ID:     "starting",
			Status: models.StatusStarting,
			RPS:    200,
		},
		{
			ID:     "running",
			Status: models.StatusRunning,
			RPS:    100,
		},
	}

	got, err := DesiredCapacity(tests, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != 3 {
		t.Fatalf("expected desired capacity 3, got %d", got)
	}
}

func TestDesiredCapacityRejectsInvalidCapacity(t *testing.T) {
	tests := []models.Test{
		{
			ID:     "test-1",
			Status: models.StatusRunning,
			RPS:    100,
		},
	}

	if _, err := DesiredCapacity(tests, 0); err == nil {
		t.Fatal("expected error for zero worker capacity")
	}
}
