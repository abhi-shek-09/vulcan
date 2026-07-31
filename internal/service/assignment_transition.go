package service

import (
	"context"
	"fmt"
)

func (ws *WorkerService) StartAssignment(
	ctx context.Context,
	testID string,
	workerID string,
) error {

	if err := ws.repo.MarkAssignmentRunning(
		ctx,
		testID,
		workerID,
	); err != nil {
		return fmt.Errorf("start assignment: %w", err)
	}

	return nil
}

func (ws *WorkerService) CompleteAssignment(
	ctx context.Context,
	testID string,
	workerID string,
) error {

	if err := ws.repo.MarkAssignmentCompleted(
		ctx,
		testID,
		workerID,
	); err != nil {
		return fmt.Errorf("complete assignment: %w", err)
	}

	return nil
}