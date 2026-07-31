package repository

import (
	"context"
	"errors"
	"fmt"
	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"

	"github.com/jackc/pgx/v5"
)

func (wr *PostgresWorkerRepository) GetReservedAssignment(ctx context.Context,workerID string) (*models.Assignment, error) {

	const query = `
		SELECT
			test_id,
			worker_id,
			status,
			assigned_at,
			started_at,
			completed_at
		FROM test_workers
		WHERE
			worker_id = $1
			AND status = 'RESERVED'
		LIMIT 1;
	`

	var assignment models.Assignment

	err := wr.db.QueryRow(ctx, query, workerID).Scan(
		&assignment.TestID,
		&assignment.WorkerID,
		&assignment.Status,
		&assignment.AssignedAt,
		&assignment.StartedAt,
		&assignment.CompletedAt,
	)

	if err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("get assignment: %w", err)
	}

	return &assignment, nil
}

func (wr *PostgresWorkerRepository) MarkAssignmentRunning(ctx context.Context, testID string, workerID string) error {

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

func (wr *PostgresWorkerRepository) MarkAssignmentCompleted(ctx context.Context, testID string, workerID string) error {
	const query = `
		UPDATE test_workers
		SET
			status = 'COMPLETED',
			completed_at = NOW()
		WHERE
			test_id = $1
			AND worker_id = $2
			AND status = 'RUNNING';
	`

	result, err := wr.db.Exec(
		ctx,
		query,
		testID,
		workerID,
	)

	if err != nil {
		return fmt.Errorf("mark assignment completed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apierrors.ErrAssignmentNotFound
	}

	return nil
}

