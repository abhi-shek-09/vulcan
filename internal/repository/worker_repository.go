package repository

import (
	"context"
	"errors"
	"fmt"
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
	UpdateHeartbeat(ctx context.Context,id string, status models.WorkerStatus) error
	MarkOfflineWorkers(ctx context.Context, cutoff time.Time) (int64, error)
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
	const query = `
		UPDATE workers
		SET
			status = $1,
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

func (wr *PostgresWorkerRepository) MarkOfflineWorkers(ctx context.Context, cutoff time.Time) (int64, error){
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