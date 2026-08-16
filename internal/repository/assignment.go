package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"
)

type AssignmentDetails struct {
	TestID      string
	WorkerID    string
	Status      models.AssignmentStatus
	AssignedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time

	TargetURL   string
	Method      string
	DurationSec int
	RPS         int
	Concurrency int
}

func (wr *PostgresWorkerRepository) GetReservedAssignment(
	ctx context.Context,
	workerID string,
) (*AssignmentDetails, error) {
	const query = `
		SELECT
			tw.test_id,
			tw.worker_id,
			tw.status,
			tw.assigned_at,
			tw.started_at,
			tw.completed_at,
			t.target_url,
			t.method,
			t.duration_sec,
			tw.rps,
			t.concurrency
		FROM test_workers tw
		JOIN tests t
			ON t.id = tw.test_id
		WHERE
			tw.worker_id = $1
			AND tw.status = 'RESERVED'
		LIMIT 1;
	`

	var assignment AssignmentDetails

	err := wr.db.QueryRow(
		ctx,
		query,
		workerID,
	).Scan(
		&assignment.TestID,
		&assignment.WorkerID,
		&assignment.Status,
		&assignment.AssignedAt,
		&assignment.StartedAt,
		&assignment.CompletedAt,
		&assignment.TargetURL,
		&assignment.Method,
		&assignment.DurationSec,
		&assignment.RPS,
		&assignment.Concurrency,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("get assignment: %w", err)
	}

	return &assignment, nil
}

func (wr *PostgresWorkerRepository) MarkAssignmentRunning(
	ctx context.Context,
	testID string,
	workerID string,
) error {
	const query = `
		UPDATE test_workers
		SET
			status = 'RUNNING',
			started_at = NOW()
		WHERE
			test_id = $1
			AND worker_id = $2
			AND status = 'RESERVED';
	`

	result, err := wr.db.Exec(
		ctx,
		query,
		testID,
		workerID,
	)
	if err != nil {
		return fmt.Errorf("mark assignment running: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apierrors.ErrAssignmentNotFound
	}

	return nil
}

func (wr *PostgresWorkerRepository) MarkAssignmentCompleted(
	ctx context.Context,
	testID string,
	workerID string,
) error {
	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin assignment completion transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	/*
		Serialize completion transactions for the same test by locking
		the parent test row.

		Without this lock, two workers can complete their assignments
		concurrently. Under PostgreSQL's default READ COMMITTED isolation,
		each transaction can see its own assignment as COMPLETED while
		still seeing the other transaction's assignment as RUNNING because
		the other transaction has not committed yet.

		Locking the parent test row first guarantees that only one
		completion transaction for a given test can perform the assignment
		completion check at a time.
	*/
	const lockTestQuery = `
		SELECT id
		FROM tests
		WHERE id = $1
		FOR UPDATE;
	`

	var lockedTestID string

	if err := tx.QueryRow(
		ctx,
		lockTestQuery,
		testID,
	).Scan(&lockedTestID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrTestNotFound
		}

		return fmt.Errorf("lock test for assignment completion: %w", err)
	}

	const completeAssignmentQuery = `
		UPDATE test_workers
		SET
			status = 'COMPLETED',
			completed_at = NOW()
		WHERE
			test_id = $1
			AND worker_id = $2
			AND status = 'RUNNING';
	`

	result, err := tx.Exec(
		ctx,
		completeAssignmentQuery,
		testID,
		workerID,
	)
	if err != nil {
		return fmt.Errorf("mark assignment completed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apierrors.ErrAssignmentNotFound
	}

	const incompleteAssignmentsQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM test_workers
			WHERE
				test_id = $1
				AND status != 'COMPLETED'
		);
	`

	var hasIncompleteAssignments bool

	if err := tx.QueryRow(
		ctx,
		incompleteAssignmentsQuery,
		testID,
	).Scan(&hasIncompleteAssignments); err != nil {
		return fmt.Errorf("check incomplete assignments: %w", err)
	}

	if !hasIncompleteAssignments {
		const completeTestQuery = `
			UPDATE tests
			SET
				status = 'COMPLETED',
				updated_at = NOW()
			WHERE
				id = $1
				AND status = 'RUNNING';
		`

		if _, err := tx.Exec(
			ctx,
			completeTestQuery,
			testID,
		); err != nil {
			return fmt.Errorf("mark test completed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit assignment completion transaction: %w", err)
	}

	return nil
}

func (wr *PostgresWorkerRepository) MarkAssignmentFailed(
	ctx context.Context,
	testID string,
	workerID string,
) error {
	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin assignment failure transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	/*
		An assignment can fail while either RESERVED or RUNNING.

		A worker can disappear after receiving an assignment but before
		successfully transitioning it to RUNNING.
	*/
	const failAssignmentQuery = `
		UPDATE test_workers
		SET
			status = 'FAILED',
			completed_at = NOW()
		WHERE
			test_id = $1
			AND worker_id = $2
			AND status IN ('RESERVED', 'RUNNING');
	`

	result, err := tx.Exec(
		ctx,
		failAssignmentQuery,
		testID,
		workerID,
	)
	if err != nil {
		return fmt.Errorf("mark assignment failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apierrors.ErrAssignmentNotFound
	}

	/*
		A failed assignment means the test cannot complete successfully.

		Transition the parent test to FAILED atomically with the
		assignment transition.
	*/
	const failTestQuery = `
		UPDATE tests
		SET
			status = 'FAILED',
			updated_at = NOW()
		WHERE
			id = $1
			AND status = 'RUNNING';
	`

	if _, err := tx.Exec(
		ctx,
		failTestQuery,
		testID,
	); err != nil {
		return fmt.Errorf("mark test failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit assignment failure transaction: %w", err)
	}

	return nil
}
