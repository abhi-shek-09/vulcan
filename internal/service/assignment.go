package service

import (
	"context"
	"fmt"

	"vulcan/internal/repository"
)

func (ws *WorkerService) GetReservedAssignment(
	ctx context.Context,
	workerID string,
) (*repository.AssignmentDetails, error) {

	assignment, err := ws.repo.GetReservedAssignment(
		ctx,
		workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("get worker assignment: %w", err)
	}

	return assignment, nil
}
