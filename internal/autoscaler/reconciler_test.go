// reconciler_test.go
package autoscaler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"vulcan/internal/models"
	"vulcan/internal/repository"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeTestRepository struct {
	tests []models.Test
	err   error
}

func (f *fakeTestRepository) GetTests(ctx context.Context) ([]models.Test, error) {
	return f.tests, f.err
}

func (f *fakeTestRepository) CreateTest(context.Context, *models.Test) error {
	return nil
}

func (f *fakeTestRepository) GetTestByID(context.Context, string) (*models.Test, error) {
	return nil, nil
}

func (f *fakeTestRepository) UpdateStatus(context.Context, string, models.TestStatus) error {
	return nil
}

func (f *fakeTestRepository) UpdateWorkerCount(context.Context, string, int) error {
	return nil
}

func (f *fakeTestRepository) TryTransitionStatus(
	context.Context,
	string,
	models.TestStatus,
	models.TestStatus,
) (bool, error) {
	return true, nil
}

type fakeWorkerRepository struct {
	workers []models.Worker

	terminateIDs []string
	terminateErr error
}

func (f *fakeWorkerRepository) GetWorkers(context.Context) ([]models.Worker, error) {
	return f.workers, nil
}

func (f *fakeWorkerRepository) TerminateIdleWorkers(
	ctx context.Context,
	workerIDs []string,
) ([]string, error) {
	if f.terminateErr != nil {
		return nil, f.terminateErr
	}

	f.terminateIDs = append([]string(nil), workerIDs...)

	return workerIDs, nil
}

func (f *fakeWorkerRepository) CreateWorker(context.Context, *models.Worker) error {
	return nil
}

func (f *fakeWorkerRepository) GetWorkerByID(context.Context, string) (*models.Worker, error) {
	return nil, nil
}

func (f *fakeWorkerRepository) UpdateHeartbeat(
	context.Context,
	string,
	models.WorkerStatus,
) error {
	return nil
}

func (f *fakeWorkerRepository) MarkOfflineWorkers(
	ctx context.Context,
	_ time.Time,
) (int64, error) {
	return 0, nil
}

func (f *fakeWorkerRepository) ReserveWorkersForTest(
	context.Context,
	string,
	int,
	[]int,
) ([]models.Worker, error) {
	return nil, nil
}

func (f *fakeWorkerRepository) ReleaseWorkersForTest(context.Context, string) error {
	return nil
}

func (f *fakeWorkerRepository) GetReservedAssignment(
	context.Context,
	string,
) (*repository.AssignmentDetails, error) {
	return nil, nil
}

func (f *fakeWorkerRepository) MarkAssignmentRunning(context.Context, string, string) error {
	return nil
}

func (f *fakeWorkerRepository) MarkAssignmentCompleted(context.Context, string, string) error {
	return nil
}

func (f *fakeWorkerRepository) MarkAssignmentFailed(context.Context, string, string) error {
	return nil
}

func (f *fakeWorkerRepository) MarkAssignmentsFailedForOfflineWorkers(
	context.Context,
) (int64, error) {
	return 0, nil
}

type fakeProvisioner struct {
	provisionCount int
	terminateIDs   []string

	provisionErr error
	terminateErr error
}

func (f *fakeProvisioner) Provision(
	_ context.Context,
	count int,
) error {
	f.provisionCount += count
	return f.provisionErr
}

func (f *fakeProvisioner) Terminate(
	_ context.Context,
	hostnames []string,
) error {
	f.terminateIDs = append([]string(nil), hostnames...)
	return f.terminateErr
}

func newWorker(id string, status models.WorkerStatus, hostname string) models.Worker {
	if hostname == "" {
		hostname = id + "-host"
	}
	return models.Worker{
		ID:       id,
		Hostname: hostname,
		Status:   status,
	}
}

func newTest(id string, status models.TestStatus, rps int) models.Test {
	return models.Test{ID: id, Status: status, RPS: rps}
}

// --- Part 10.1: scale up ---

func TestReconcileScalesUp(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusRunning, 400)}, // ceil(400/100) = 4
	}
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
			newWorker("w2", models.WorkerStatusIdle, "vulcan-auto-worker-2"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prov.provisionCount != 2 {
		t.Fatalf("expected provision(2), got provision(%d)", prov.provisionCount)
	}
	if len(prov.terminateIDs) != 0 {
		t.Fatalf("expected no termination, got %v", prov.terminateIDs)
	}
}

// --- Part 10.2: no scaling ---

func TestReconcileNoScaling(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusRunning, 200)}, // ceil(200/100) = 2
	}
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusRunning, "vulcan-auto-worker-1"),
			newWorker("w2", models.WorkerStatusRunning, "vulcan-auto-worker-2"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prov.provisionCount != 0 {
		t.Fatalf("expected no provisioning, got %d", prov.provisionCount)
	}
	if len(prov.terminateIDs) != 0 {
		t.Fatalf("expected no termination, got %v", prov.terminateIDs)
	}
	if len(workerRepo.terminateIDs) != 0 {
		t.Fatalf("expected no drain request, got %v", workerRepo.terminateIDs)
	}
}

// --- Part 10.3: scale down ---

func TestReconcileScalesDown(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusRunning, 100)}, // ceil(100/100) = 1
	}
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
			newWorker("w2", models.WorkerStatusIdle, "vulcan-auto-worker-2"),
			newWorker("w3", models.WorkerStatusIdle, "vulcan-auto-worker-3"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(prov.terminateIDs) != 2 {
		t.Fatalf("expected 2 workers terminated, got %d (%v)", len(prov.terminateIDs), prov.terminateIDs)
	}
	if prov.provisionCount != 0 {
		t.Fatalf("expected no provisioning, got %d", prov.provisionCount)
	}
}

// --- Part 10.4: active workers protected ---

func TestReconcileProtectsActiveWorkers(t *testing.T) {
	testRepo := &fakeTestRepository{} // no active tests -> desired == 0
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("running", models.WorkerStatusRunning, "vulcan-auto-worker-running"),
			newWorker("reserved", models.WorkerStatusReserved, "vulcan-auto-worker-reserved"),
			newWorker("idle", models.WorkerStatusIdle, "vulcan-auto-worker-idle"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(workerRepo.terminateIDs) != 1 || workerRepo.terminateIDs[0] != "idle" {
		t.Fatalf("expected only the idle worker to be drained, got %v", workerRepo.terminateIDs)
	}
	if len(prov.terminateIDs) != 1 || prov.terminateIDs[0] != "vulcan-auto-worker-idle" {
		t.Fatalf("expected only the idle worker's process terminated, got %v", prov.terminateIDs)
	}
}

// --- Part 10.4b: a pending (CREATED, not-yet-started) test protects the
// idle worker it will need from being drained as excess capacity. This is
// the Phase 10 lifecycle bug: previously DesiredCapacity (and therefore the
// scale-down decision) only looked at STARTING/RUNNING tests, so a freshly
// created test sitting at CREATED contributed zero to "desired", the sole
// idle worker looked like pure excess, and the reconciler drained it before
// the test ever got a chance to call /start and reserve it. ---

func TestReconcileProtectsWorkersForPendingTests(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusCreated, 100)}, // ceil(100/100) = 1, desired == 0
	}
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(workerRepo.terminateIDs) != 0 {
		t.Fatalf("expected the worker needed by the pending test to survive, got drained: %v", workerRepo.terminateIDs)
	}
	if prov.provisionCount != 0 {
		t.Fatalf("a CREATED test must not itself trigger provisioning, got %d", prov.provisionCount)
	}
}

// A pending test only protects the workers it actually needs -- any
// idle capacity beyond that is still genuine excess and must still be
// drained.
func TestReconcilePendingTestDoesNotProtectExcessBeyondItsNeed(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusCreated, 100)}, // needs 1 worker
	}
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
			newWorker("w2", models.WorkerStatusIdle, "vulcan-auto-worker-2"),
			newWorker("w3", models.WorkerStatusIdle, "vulcan-auto-worker-3"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(workerRepo.terminateIDs) != 2 {
		t.Fatalf("expected 2 excess workers drained beyond the 1 the pending test needs, got %d (%v)", len(workerRepo.terminateIDs), workerRepo.terminateIDs)
	}
}

// --- Part 10.5: multiple active tests ---

func TestReconcileMultipleActiveTests(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{
			newTest("a", models.StatusRunning, 250),  // ceil(250/100) = 3
			newTest("b", models.StatusStarting, 150), // ceil(150/100) = 2
		},
	}
	workerRepo := &fakeWorkerRepository{}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prov.provisionCount != 5 {
		t.Fatalf("expected desired capacity 5, provisioned %d", prov.provisionCount)
	}
}

// --- Part 10.6: provisioning failure ---

func TestReconcileProvisioningFailure(t *testing.T) {
	testRepo := &fakeTestRepository{
		tests: []models.Test{newTest("t1", models.StatusRunning, 400)},
	}
	workerRepo := &fakeWorkerRepository{}
	prov := &fakeProvisioner{provisionErr: errors.New("provision boom")}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err == nil {
		t.Fatal("expected Reconcile to return an error")
	}
}

// --- Part 10.7: termination failure ---

func TestReconcileTerminationFailure(t *testing.T) {
	testRepo := &fakeTestRepository{} // desired == 0
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1")},
	}
	prov := &fakeProvisioner{terminateErr: errors.New("terminate boom")}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err == nil {
		t.Fatal("expected Reconcile to return an error")
	}

	// The atomic IDLE -> DRAINING transition must still have happened
	// (the repository call succeeded) even though the process termination
	// failed afterwards -- the worker is protected from reassignment
	// either way.
	if len(workerRepo.terminateIDs) != 1 || workerRepo.terminateIDs[0] != "w1" {
		t.Fatalf("expected worker w1 to have been marked draining, got %v", workerRepo.terminateIDs)
	}
}

// --- Part 10.8: context cancellation stops the loop ---

func TestRunStopsOnContextCancellation(t *testing.T) {
	testRepo := &fakeTestRepository{}
	workerRepo := &fakeWorkerRepository{}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, 5*time.Millisecond)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// --- Part 10.9: reconciliation cycles never overlap ---

type overlapTrackingWorkerRepo struct {
	fakeWorkerRepository

	inFlight int32
	overlap  int32
	calls    int32
	delay    time.Duration
}

func (r *overlapTrackingWorkerRepo) GetWorkers(ctx context.Context) ([]models.Worker, error) {
	if atomic.AddInt32(&r.inFlight, 1) > 1 {
		atomic.StoreInt32(&r.overlap, 1)
	}
	atomic.AddInt32(&r.calls, 1)

	time.Sleep(r.delay)

	atomic.AddInt32(&r.inFlight, -1)

	return r.fakeWorkerRepository.GetWorkers(ctx)
}

func TestRunDoesNotOverlapReconciliations(t *testing.T) {
	testRepo := &fakeTestRepository{}
	workerRepo := &overlapTrackingWorkerRepo{delay: 40 * time.Millisecond}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, 5*time.Millisecond)
	}()

	// Each cycle takes 40ms; ticking every 5ms would fire ~30 times in
	// 150ms if cycles overlapped. A synchronous loop only manages ~3-4.
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	if atomic.LoadInt32(&workerRepo.overlap) != 0 {
		t.Fatal("detected overlapping reconciliation cycles")
	}

	calls := atomic.LoadInt32(&workerRepo.calls)
	if calls < 2 {
		t.Fatalf("expected the loop to have reconciled more than once, got %d calls", calls)
	}
}

// --- Part 10.10: a failed reconciliation does not stop the loop ---

type failOnceTestRepository struct {
	fakeTestRepository
	calls int32
}

func (f *failOnceTestRepository) GetTests(ctx context.Context) ([]models.Test, error) {
	if atomic.AddInt32(&f.calls, 1) == 1 {
		return nil, errors.New("transient failure")
	}
	return f.fakeTestRepository.GetTests(ctx)
}

func TestRunContinuesAfterReconciliationFailure(t *testing.T) {
	testRepo := &failOnceTestRepository{}
	workerRepo := &fakeWorkerRepository{}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, 10*time.Millisecond)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	calls := atomic.LoadInt32(&testRepo.calls)
	if calls < 2 {
		t.Fatalf("expected the loop to keep reconciling after a failure, only saw %d calls", calls)
	}
}

// --- NEW TESTS: ownership filter ---

// TestReconcileDoesNotScaleDownComposeWorker verifies that the autoscaler
// never selects a non-autoscaler-managed worker (worker-compose-1) for
// scale-down.
func TestReconcileDoesNotScaleDownComposeWorker(t *testing.T) {
	testRepo := &fakeTestRepository{} // desired == 0
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			// This worker has the exact hostname pattern that caused the bug.
			// It was started by Docker Compose and is not owned by the autoscaler.
			newWorker("w1", models.WorkerStatusIdle, "worker-compose-1"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(workerRepo.terminateIDs) != 0 {
		t.Fatalf("expected compose worker to NOT be drained, got drained: %v", workerRepo.terminateIDs)
	}
	if len(prov.terminateIDs) != 0 {
		t.Fatalf("expected no termination, got %v", prov.terminateIDs)
	}
}

// TestReconcileScalesDownAutoscalerManagedWorker verifies that the autoscaler
// correctly selects autoscaler-managed workers (vulcan-auto-worker-*) for
// scale-down when they are idle and excess.
func TestReconcileScalesDownAutoscalerManagedWorker(t *testing.T) {
	testRepo := &fakeTestRepository{} // desired == 0
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			newWorker("w1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(workerRepo.terminateIDs) != 1 {
		t.Fatalf("expected autoscaler-managed worker to be drained, got %d drained", len(workerRepo.terminateIDs))
	}
	if workerRepo.terminateIDs[0] != "w1" {
		t.Fatalf("expected worker w1 to be drained, got %v", workerRepo.terminateIDs)
	}
	if len(prov.terminateIDs) != 1 {
		t.Fatalf("expected 1 termination, got %d", len(prov.terminateIDs))
	}
	if prov.terminateIDs[0] != "vulcan-auto-worker-1" {
		t.Fatalf("expected termination of vulcan-auto-worker-1, got %v", prov.terminateIDs)
	}
}

// TestReconcileScalesDownOnlyAutoscalerManagedWorkers verifies that when
// both autoscaler-managed and non-autoscaler-managed idle workers exist,
// only the autoscaler-managed ones are selected for scale-down.
func TestReconcileScalesDownOnlyAutoscalerManagedWorkers(t *testing.T) {
	testRepo := &fakeTestRepository{} // desired == 0
	workerRepo := &fakeWorkerRepository{
		workers: []models.Worker{
			// This compose worker should NEVER be selected for scale-down.
			newWorker("compose", models.WorkerStatusIdle, "worker-compose-1"),
			// These autoscaler-managed workers should be selected.
			newWorker("auto1", models.WorkerStatusIdle, "vulcan-auto-worker-1"),
			newWorker("auto2", models.WorkerStatusIdle, "vulcan-auto-worker-2"),
		},
	}
	prov := &fakeProvisioner{}

	r := NewReconciler(testRepo, workerRepo, prov, 100, testLogger())

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With desired=0 and 3 idle workers, excess is 3. However, only 2 of
	// those workers are autoscaler-managed and eligible for scale-down.
	// The compose worker should be protected.
	if len(workerRepo.terminateIDs) != 2 {
		t.Fatalf("expected 2 autoscaler-managed workers to be drained, got %d (%v)", len(workerRepo.terminateIDs), workerRepo.terminateIDs)
	}

	// Verify the compose worker was NOT selected.
	for _, id := range workerRepo.terminateIDs {
		if id == "compose" {
			t.Fatalf("compose worker was incorrectly selected for scale-down")
		}
	}

	// Verify the autoscaler-managed workers WERE selected.
	selected := make(map[string]bool)
	for _, id := range workerRepo.terminateIDs {
		selected[id] = true
	}
	if !selected["auto1"] || !selected["auto2"] {
		t.Fatalf("expected auto1 and auto2 to be drained, got %v", workerRepo.terminateIDs)
	}

	if len(prov.terminateIDs) != 2 {
		t.Fatalf("expected 2 terminations, got %d", len(prov.terminateIDs))
	}
}
