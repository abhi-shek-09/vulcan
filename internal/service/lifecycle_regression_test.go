package service_test

// Regression test for the Phase 10 worker-reuse bug:
//
//   1. create test A
//   2. start test A
//   3. complete test A
//   4. worker returns to IDLE
//   5. create test B
//   6. start test B
//   7. test B must successfully transition to RUNNING
//
// Root cause: PostgresWorkerRepository.MarkAssignmentRunning updated the
// test_workers row from RESERVED -> RUNNING but never updated the
// corresponding workers row from RESERVED -> RUNNING. MarkAssignmentCompleted
// later tried to flip the worker RUNNING -> IDLE, but since the worker was
// actually still sitting at RESERVED, that conditional UPDATE matched zero
// rows (and the result was never checked), so the worker was silently
// stranded at RESERVED forever after its first assignment. The next test's
// /start call would then see zero IDLE workers, wait for
// ensureWorkersAvailable's 30s timeout, and fail with a generic 500 -- while
// the test itself rolled back to CREATED and the worker showed
// RESERVED/assigned=false, exactly matching the reported symptom.
//
// This test exercises the real Postgres-backed repositories (not fakes) so
// that a regression in the worker-status transition inside a repository
// transaction is actually caught. It requires a reachable Postgres instance;
// set TEST_DATABASE_URL (falling back to DB_URL, then a sane local default)
// to point at a scratch database. If none is reachable, the test is skipped
// rather than failed, so `go test ./...` still passes in environments
// without Postgres.

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"vulcan/internal/models"
	"vulcan/internal/repository"
	"vulcan/internal/scheduler"
	"vulcan/internal/service"
)

func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DB_URL"); v != "" {
		return v
	}
	return "postgresql://vulcandev:vulcanpassword@localhost:5432/vulcandb"
}

func mustConnectPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), testDatabaseURL())
	if err != nil {
		t.Skipf("skipping: could not create postgres pool: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping: postgres not reachable: %v", err)
	}

	return pool
}

func newULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// TestWorkerCanBeReusedAcrossTests reproduces the exact create/start/
// complete/reuse sequence from the Phase 10 handover.
func TestWorkerCanBeReusedAcrossTests(t *testing.T) {
	pool := mustConnectPool(t)
	defer pool.Close()

	ctx := context.Background()

	testRepo := repository.NewTestRepository(pool)
	workerRepo := repository.NewWorkerRepository(pool)
	sched := scheduler.NewDefaultScheduler(workerRepo)

	const workerCapacityRPS = 100
	svc := service.NewTestService(testRepo, sched, workerRepo, nil, workerCapacityRPS, nil)

	// Register a single worker, mirroring the single Compose worker in the
	// Phase 10 deployment.
	workerID := newULID()
	now := time.Now().UTC()
	if err := workerRepo.CreateWorker(ctx, &models.Worker{
		ID:            workerID,
		Hostname:      "regression-test-worker",
		Version:       "test",
		Status:        models.WorkerStatusIdle,
		CPUCount:      1,
		MemoryMB:      512,
		RegisteredAt:  now,
		LastHeartbeat: now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	createAndStart := func(name string) *models.Test {
		t.Helper()

		test := &models.Test{
			ID:          newULID(),
			Name:        name,
			Status:      models.StatusCreated,
			TargetURL:   "http://example.invalid",
			Method:      "GET",
			DurationSec: 1,
			RPS:         10,
			Concurrency: 1,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}

		if err := testRepo.CreateTest(ctx, test); err != nil {
			t.Fatalf("create test %s: %v", name, err)
		}

		if _, err := svc.StartTest(ctx, test.ID); err != nil {
			t.Fatalf("start test %s: %v", name, err)
		}

		return test
	}

	// --- Test A: create, start, run to completion ---

	testA := createAndStart("regression-test-a")

	if err := workerRepo.MarkAssignmentRunning(ctx, testA.ID, workerID); err != nil {
		t.Fatalf("mark assignment running for test A: %v", err)
	}

	// This is the exact assertion that catches the bug: after
	// MarkAssignmentRunning, the worker itself (not just the assignment)
	// must have transitioned to RUNNING.
	workerAfterRunning, err := workerRepo.GetWorkerByID(ctx, workerID)
	if err != nil {
		t.Fatalf("get worker after marking running: %v", err)
	}
	if workerAfterRunning.Status != models.WorkerStatusRunning {
		t.Fatalf(
			"expected worker status RUNNING after MarkAssignmentRunning, got %s",
			workerAfterRunning.Status,
		)
	}

	if err := workerRepo.MarkAssignmentCompleted(ctx, testA.ID, workerID); err != nil {
		t.Fatalf("mark assignment completed for test A: %v", err)
	}

	// The worker must return to IDLE, not stay stranded at RESERVED/RUNNING.
	workerAfterCompletion, err := workerRepo.GetWorkerByID(ctx, workerID)
	if err != nil {
		t.Fatalf("get worker after completion: %v", err)
	}
	if workerAfterCompletion.Status != models.WorkerStatusIdle {
		t.Fatalf(
			"expected worker status IDLE after test A completed, got %s",
			workerAfterCompletion.Status,
		)
	}

	testAAfter, err := testRepo.GetTestByID(ctx, testA.ID)
	if err != nil {
		t.Fatalf("get test A: %v", err)
	}
	if testAAfter.Status != models.StatusCompleted {
		t.Fatalf("expected test A COMPLETED, got %s", testAAfter.Status)
	}

	// --- Test B: create, start -- must successfully reuse the same worker ---

	testB := createAndStart("regression-test-b")

	testBAfter, err := testRepo.GetTestByID(ctx, testB.ID)
	if err != nil {
		t.Fatalf("get test B: %v", err)
	}
	if testBAfter.Status != models.StatusRunning {
		t.Fatalf(
			"expected test B to reach RUNNING after start, got %s (this is the reported bug: "+
				"the test stays CREATED because no IDLE worker was ever found)",
			testBAfter.Status,
		)
	}

	assignment, err := workerRepo.GetReservedAssignment(ctx, workerID)
	if err != nil {
		t.Fatalf("get reserved assignment for test B: %v", err)
	}
	if assignment == nil {
		t.Fatalf("expected a RESERVED assignment for test B, got none (assigned=false)")
	}
	if assignment.TestID != testB.ID {
		t.Fatalf("expected assignment for test B (%s), got %s", testB.ID, assignment.TestID)
	}

	// Cleanup: release the worker so this test can be re-run without
	// manual intervention.
	if err := workerRepo.MarkAssignmentRunning(ctx, testB.ID, workerID); err != nil {
		t.Fatalf("mark assignment running for test B (cleanup): %v", err)
	}
	if err := workerRepo.MarkAssignmentCompleted(ctx, testB.ID, workerID); err != nil {
		t.Fatalf("mark assignment completed for test B (cleanup): %v", err)
	}
}
