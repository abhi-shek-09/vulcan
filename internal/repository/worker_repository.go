package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkerRepository interface {
	CreateWorker(ctx context.Context, worker *models.Worker) error
	GetWorkers(ctx context.Context) ([]models.Worker, error)
	GetWorkerByID(ctx context.Context, id string) (*models.Worker, error)

	UpdateHeartbeat(
		ctx context.Context,
		id string,
		status models.WorkerStatus,
	) error

	MarkOfflineWorkers(
		ctx context.Context,
		cutoff time.Time,
	) (int64, error)

	ReserveWorkersForTest(
		ctx context.Context,
		testID string,
		workerCount int,
		rpsAllocations []int,
	) ([]models.Worker, error)

	ReleaseWorkersForTest(
		ctx context.Context,
		testID string,
	) error

	GetReservedAssignment(
		ctx context.Context,
		workerID string,
	) (*AssignmentDetails, error)

	MarkAssignmentRunning(
		ctx context.Context,
		testID string,
		workerID string,
	) error

	MarkAssignmentCompleted(
		ctx context.Context,
		testID string,
		workerID string,
	) error

	MarkAssignmentFailed(
		ctx context.Context,
		testID string,
		workerID string,
	) error

	MarkAssignmentsFailedForOfflineWorkers(
		ctx context.Context,
	) (int64, error)

	TerminateIdleWorkers(
		ctx context.Context,
		workerIDs []string,
	) ([]string, error)
}

type PostgresWorkerRepository struct {
	db *pgxpool.Pool
}

func NewWorkerRepository(db *pgxpool.Pool) WorkerRepository {
	return &PostgresWorkerRepository{
		db: db,
	}
}

func (wr *PostgresWorkerRepository) CreateWorker(ctx context.Context, worker *models.Worker) error {

	const query = `	
		INSERT INTO workers (
			id,
			hostname,
			version,
			status,
			cpu_count,
			memory_mb,
			registered_at,
			last_heartbeat,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := wr.db.Exec(
		ctx,
		query,
		worker.ID,
		worker.Hostname,
		worker.Version,
		worker.Status,
		worker.CPUCount,
		worker.MemoryMB,
		worker.RegisteredAt,
		worker.LastHeartbeat,
		worker.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create worker: %w", err)
	}

	return nil
}

func (wr *PostgresWorkerRepository) GetWorkers(ctx context.Context) ([]models.Worker, error) {
	const query = `
		SELECT
			id,
			hostname,
			version,
			status,
			cpu_count,
			memory_mb,
			registered_at,
			last_heartbeat,
			updated_at
		FROM workers
		ORDER BY registered_at DESC
	`

	rows, err := wr.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer rows.Close()

	var workers []models.Worker

	for rows.Next() {
		var worker models.Worker

		if err := rows.Scan(
			&worker.ID,
			&worker.Hostname,
			&worker.Version,
			&worker.Status,
			&worker.CPUCount,
			&worker.MemoryMB,
			&worker.RegisteredAt,
			&worker.LastHeartbeat,
			&worker.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}

		workers = append(workers, worker)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workers: %w", err)
	}

	return workers, nil
}

func (wr *PostgresWorkerRepository) GetWorkerByID(ctx context.Context, id string) (*models.Worker, error) {
	const query = `
		SELECT
			id,
			hostname,
			version,
			status,
			cpu_count,
			memory_mb,
			registered_at,
			last_heartbeat,
			updated_at
		FROM workers
		WHERE id = $1
	`

	var worker models.Worker

	err := wr.db.QueryRow(ctx, query, id).Scan(
		&worker.ID,
		&worker.Hostname,
		&worker.Version,
		&worker.Status,
		&worker.CPUCount,
		&worker.MemoryMB,
		&worker.RegisteredAt,
		&worker.LastHeartbeat,
		&worker.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.ErrWorkerNotFound
		}

		return nil, fmt.Errorf("get worker: %w", err)
	}

	return &worker, nil
}

func (wr *PostgresWorkerRepository) UpdateHeartbeat(ctx context.Context, id string, status models.WorkerStatus) error {
	// A worker only knows its own local execution state (IDLE while
	// waiting, RUNNING while executing an assignment). It has no way of
	// knowing that the control plane has independently reserved it for a
	// test (RESERVED), decided to drain it (DRAINING), or already
	// declared it OFFLINE after a missed-heartbeat timeout. Those are
	// server-authoritative states.
	//
	// If a heartbeat's self-reported status were allowed to blindly
	// overwrite one of those states, a worker sitting idle between being
	// reserved and picking up its assignment on the next poll tick would
	// flip itself back to IDLE mid-reservation, and a worker the
	// autoscaler just marked DRAINING would immediately un-drain itself
	// on its next heartbeat -- silently violating the scale-down safety
	// invariant that a reserved/draining worker must never be handed
	// back out. last_heartbeat/updated_at are still always refreshed so
	// liveness tracking (MarkOfflineWorkers) keeps working regardless.
	const query = `
		UPDATE workers
		SET
			status = CASE
				WHEN status IN ('RESERVED', 'DRAINING', 'OFFLINE') THEN status
				ELSE $1
			END,
			last_heartbeat = $2,
			updated_at = $2
		WHERE id = $3
	`

	now := time.Now().UTC()

	result, err := wr.db.Exec(
		ctx,
		query,
		status,
		now,
		id,
	)

	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apierrors.ErrWorkerNotFound
	}

	return nil
}

func (wr *PostgresWorkerRepository) MarkOfflineWorkers(ctx context.Context, cutoff time.Time) (int64, error) {
	const query = `
		UPDATE workers
		SET
			status = 'OFFLINE',
			updated_at = NOW()
		WHERE
			status != 'OFFLINE'
			AND last_heartbeat < $1;
	`
	result, err := wr.db.Exec(
		ctx,
		query,
		cutoff,
	)

	if err != nil {
		return 0, fmt.Errorf("mark worker offline: %w", err)
	}

	return result.RowsAffected(), nil
}

func (wr *PostgresWorkerRepository) ReserveWorkersForTest(
	ctx context.Context,
	testID string,
	workerCount int,
	rpsAllocations []int,
) ([]models.Worker, error) {

	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reservation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const selectQuery = `
		SELECT
			id,
			hostname,
			version,
			status,
			cpu_count,
			memory_mb,
			registered_at,
			last_heartbeat,
			updated_at
		FROM workers
		WHERE status = 'IDLE'
		ORDER BY last_heartbeat DESC
		FOR UPDATE SKIP LOCKED
		LIMIT $1;
	`

	rows, err := tx.Query(ctx, selectQuery, workerCount)
	if err != nil {
		return nil, fmt.Errorf("lock idle workers: %w", err)
	}
	defer rows.Close()

	var (
		workerIDs       []string
		reservedWorkers []models.Worker
	)

	for rows.Next() {
		var w models.Worker

		if err := rows.Scan(
			&w.ID,
			&w.Hostname,
			&w.Version,
			&w.Status,
			&w.CPUCount,
			&w.MemoryMB,
			&w.RegisteredAt,
			&w.LastHeartbeat,
			&w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}

		workerIDs = append(workerIDs, w.ID)
		reservedWorkers = append(reservedWorkers, w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked workers: %w", err)
	}

	if len(workerIDs) < workerCount {
		return nil, apierrors.ErrInsufficientWorkers
	}

	const updateQuery = `
		UPDATE workers
		SET
			status = 'RESERVED',
			updated_at = NOW()
		WHERE id = ANY($1);
	`

	if _, err := tx.Exec(ctx, updateQuery, workerIDs); err != nil {
		return nil, fmt.Errorf("reserve workers: %w", err)
	}

	for i := range reservedWorkers {
		reservedWorkers[i].Status = models.WorkerStatusReserved
	}
	// Step 3: Create test-worker mappings
	var (
		values       = []interface{}{testID}
		placeholders []string
	)

	if len(rpsAllocations) != len(reservedWorkers) {
		return nil, fmt.Errorf("rps allocation count %d does not match worker count %d", len(rpsAllocations), len(reservedWorkers))
	}

	for i, worker := range reservedWorkers {
		placeholders = append(
			placeholders,
			fmt.Sprintf("($1, $%d, 'RESERVED', NOW(), $%d)", i+2, i+2+len(reservedWorkers)),
		)
		values = append(values, worker.ID)
	}

	for _, rps := range rpsAllocations {
		values = append(values, rps)
	}

	// insert into test workers table => instead of one by one insert, we do a bulk insert
	// INSERT INTO test_workers (test_id, worker_id, assigned_at)
	// VALUES ($1, $2, NOW()), ($1, $3, NOW()), ($1, $4, NOW());
	insertQuery := fmt.Sprintf(`
		INSERT INTO test_workers (
			test_id,
			worker_id,
			status,
			assigned_at,
			rps
		)
		VALUES %s;
	`, strings.Join(placeholders, ","))

	if _, err := tx.Exec(ctx, insertQuery, values...); err != nil {
		return nil, fmt.Errorf("create test-worker mappings: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reservation transaction: %w", err)
	}

	return reservedWorkers, nil
}

func (wr *PostgresWorkerRepository) ReleaseWorkersForTest(ctx context.Context, testID string) error {
	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin release transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const selectQuery = `
		SELECT worker_id
		FROM test_workers
		WHERE test_id = $1
		FOR UPDATE;
	`

	rows, err := tx.Query(ctx, selectQuery, testID)
	if err != nil {
		return fmt.Errorf("lock test-worker mappings: %w", err)
	}
	defer rows.Close()

	var workerIDs []string

	for rows.Next() {
		var workerID string

		if err := rows.Scan(&workerID); err != nil {
			return fmt.Errorf("scan assigned worker id: %w", err)
		}

		workerIDs = append(workerIDs, workerID)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate assigned worker mappings: %w", err)
	}

	if len(workerIDs) == 0 {
		return tx.Commit(ctx)
	}

	// Release workers back to IDLE.
	const updateWorkersQuery = `
		UPDATE workers
		SET
			status = 'IDLE',
			updated_at = NOW()
		WHERE
			id = ANY($1)
			AND status = 'RESERVED';
	`

	if _, err := tx.Exec(ctx, updateWorkersQuery, workerIDs); err != nil {
		return fmt.Errorf("release workers: %w", err)
	}

	// Mark assignments as completed instead of deleting them.
	const completeAssignmentsQuery = `
		UPDATE test_workers
		SET
			status = 'COMPLETED',
			completed_at = NOW()
		WHERE
			test_id = $1
			AND status IN ('RESERVED', 'RUNNING');
	`

	if _, err := tx.Exec(ctx, completeAssignmentsQuery, testID); err != nil {
		return fmt.Errorf("complete test-worker assignments: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit release transaction: %w", err)
	}

	return nil
}

func (wr *PostgresWorkerRepository) MarkAssignmentsFailedForOfflineWorkers(
	ctx context.Context,
) (int64, error) {
	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf(
			"begin offline assignment reconciliation transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	/*
		Any RESERVED/RUNNING assignment belonging to an OFFLINE worker
		can no longer complete successfully.

		Mark it FAILED.
	*/

	const failAssignmentsQuery = `
		UPDATE test_workers tw
		SET
			status = 'FAILED',
			completed_at = NOW()
		FROM workers w
		WHERE
			tw.worker_id = w.id
			AND w.status = 'OFFLINE'
			AND tw.status IN ('RESERVED', 'RUNNING');
	`

	result, err := tx.Exec(
		ctx,
		failAssignmentsQuery,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"fail assignments for offline workers: %w",
			err,
		)
	}

	affected := result.RowsAffected()

	/*
		Any test with a failed assignment must be FAILED.

		We only touch RUNNING tests.
	*/

	const failTestsQuery = `
		UPDATE tests t
		SET
			status = 'FAILED',
			updated_at = NOW()
		WHERE
			t.status = 'RUNNING'
			AND EXISTS (
				SELECT 1
				FROM test_workers tw
				WHERE
					tw.test_id = t.id
					AND tw.status = 'FAILED'
			);
	`

	if _, err := tx.Exec(
		ctx,
		failTestsQuery,
	); err != nil {
		return 0, fmt.Errorf(
			"fail tests for offline workers: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf(
			"commit offline assignment reconciliation transaction: %w",
			err,
		)
	}

	return affected, nil
}

func (wr *PostgresWorkerRepository) TerminateIdleWorkers(
	ctx context.Context,
	workerIDs []string,
) ([]string, error) {
	if len(workerIDs) == 0 {
		return nil, nil
	}

	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin worker termination transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the workers and verify they are still IDLE.
	// A scheduler trying to reserve one of these workers must
	// acquire the same row lock, so only one operation can win.
	const selectQuery = `
		SELECT id
		FROM workers
		WHERE id = ANY($1)
		  AND status = 'IDLE'
		FOR UPDATE;
	`

	rows, err := tx.Query(ctx, selectQuery, workerIDs)
	if err != nil {
		return nil, fmt.Errorf("lock idle workers for termination: %w", err)
	}
	defer rows.Close()

	var removable []string

	for rows.Next() {
		var workerID string

		if err := rows.Scan(&workerID); err != nil {
			return nil, fmt.Errorf("scan worker for termination: %w", err)
		}

		removable = append(removable, workerID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workers for termination: %w", err)
	}

	if len(removable) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty termination transaction: %w", err)
		}

		return nil, nil
	}

	// Mark them DRAINING while we still hold the row locks.
	const updateQuery = `
		UPDATE workers
		SET
			status = 'DRAINING',
			updated_at = NOW()
		WHERE id = ANY($1);
	`

	if _, err := tx.Exec(ctx, updateQuery, removable); err != nil {
		return nil, fmt.Errorf("mark workers draining: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit worker termination transaction: %w", err)
	}

	return removable, nil
}
