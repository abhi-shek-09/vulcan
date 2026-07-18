package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"strings"
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
	ReserveWorkersForTest(ctx context.Context, testID string, workerCount int) ([]models.Worker, error)
	ReleaseWorkersForTest(ctx context.Context,testID string,) error
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

func (wr *PostgresWorkerRepository) ReserveWorkersForTest(ctx context.Context, testID string, workerCount int) ([]models.Worker, error) {

	tx, err := wr.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reservation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const selectQuery = `
		SELECT id
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

	var workerIDs []string

	for rows.Next() {
		var id string

		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan worker id: %w", err)
		}

		workerIDs = append(workerIDs, id)
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
		WHERE id = ANY($1)
		RETURNING
			id,
			hostname,
			version,
			status,
			cpu_count,
			memory_mb,
			registered_at,
			last_heartbeat,
			updated_at;
	`

	updateRows, err := tx.Query(ctx, updateQuery, workerIDs)
	if err != nil {
		return nil, fmt.Errorf("reserve workers: %w", err)
	}
	defer updateRows.Close()

	var reservedWorkers []models.Worker

	for updateRows.Next() {
		var w models.Worker

		if err := updateRows.Scan(
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
			return nil, fmt.Errorf("scan reserved worker: %w", err)
		}

		reservedWorkers = append(reservedWorkers, w)
	}

	if err := updateRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reserved workers: %w", err)
	}

	// Step 3: Create test-worker mappings
	var (
		values       = []interface{}{testID}
		placeholders []string
	)

	for i, worker := range reservedWorkers {
		placeholders = append(
			placeholders,
			fmt.Sprintf("($1, $%d, NOW())", i+2),
		)

		values = append(values, worker.ID)
	}

	// insert into test workers table => instead of one by one insert, we do a bulk insert
	// INSERT INTO test_workers (test_id, worker_id, assigned_at) 
	// VALUES ($1, $2, NOW()), ($1, $3, NOW()), ($1, $4, NOW());
	insertQuery := fmt.Sprintf(`
		INSERT INTO test_workers (
			test_id,
			worker_id,
			assigned_at
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

func (wr *PostgresWorkerRepository) ReleaseWorkersForTest(ctx context.Context,testID string,) error{
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
        // return nil - this will return without committing the transaction
		return tx.Commit(ctx)
	}

    // Transition the workers back to an IDLE status state
    const updateQuery = `
        UPDATE workers
		SET
			status = 'IDLE',
			updated_at = NOW()
		WHERE
			id = ANY($1)
			AND status = 'RESERVED';
    `

    if _, err := tx.Exec(ctx, updateQuery, workerIDs); err != nil {
        return fmt.Errorf("update workers status to idle: %w", err)
    }

    // Delete the mapping records out of the link table safely
    const deleteQuery = `
        DELETE FROM test_workers
        WHERE test_id = $1;
    `

    if _, err := tx.Exec(ctx, deleteQuery, testID); err != nil {
        return fmt.Errorf("delete test-worker mappings: %w", err)
    }

    // Finalize all operations to disk atomically
    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("commit release transaction: %w", err)
    }

    return nil
}