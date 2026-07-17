package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"
)

type TestRepository interface {
	CreateTest(ctx context.Context, test *models.Test) error
	GetTests(ctx context.Context) ([]models.Test, error)
	GetTestByID(ctx context.Context, id string) (*models.Test, error)
	UpdateStatus(ctx context.Context, id string, status models.TestStatus) error
}

type PostgresTestRepository struct {
	db *pgxpool.Pool
}

func NewTestRepository(db *pgxpool.Pool) TestRepository {
	return &PostgresTestRepository{
		db: db,
	}
}

func (r *PostgresTestRepository) CreateTest(ctx context.Context, test *models.Test) error {
	const query = `
		INSERT INTO tests (
			id,
			name,
			status,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5)
	` // avoiding sql injection

	_, err := r.db.Exec(
		ctx,
		query,
		test.ID,
		test.Name,
		test.Status,
		test.CreatedAt,
		test.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("create test: %w", err)
	}

	return nil
}

func (r *PostgresTestRepository) GetTests(ctx context.Context) ([]models.Test, error) {
	const query = `
		SELECT
			id,
			name,
			status,
			created_at,
			updated_at
		FROM tests
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tests: %w", err)
	}
	defer rows.Close()

	var tests []models.Test

	for rows.Next() {
		var test models.Test

		if err := rows.Scan(
			&test.ID,
			&test.Name,
			&test.Status,
			&test.CreatedAt,
			&test.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan test: %w", err)
		}

		tests = append(tests, test)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tests: %w", err)
	}

	return tests, nil
}

func (r *PostgresTestRepository) GetTestByID(ctx context.Context, id string) (*models.Test, error) {
	const query = `
		SELECT
			id,
			name,
			status,
			created_at,
			updated_at
		FROM tests
		WHERE id = $1
	`

	var test models.Test

	err := r.db.QueryRow(ctx, query, id).Scan(
		&test.ID,
		&test.Name,
		&test.Status,
		&test.CreatedAt,
		&test.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.ErrTestNotFound
		}

		return nil, fmt.Errorf("get test: %w", err)
	}

	return &test, nil
}

func (r *PostgresTestRepository) UpdateStatus(ctx context.Context, id string, status models.TestStatus) error {
	const query = `
		UPDATE tests
		SET
			status = $2,
			updated_at = NOW()
		WHERE id = $1
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		id,
		status,
	)

	if err != nil {
		return fmt.Errorf("update test status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return apierrors.ErrTestNotFound
	}

	return nil
}
