package worker

import (
	"context"
	"time"

	"vulcan/internal/controlplane"
)

func (w *Worker) execute(ctx context.Context, assignment *controlplane.AssignmentResponse) error {

	w.logger.Info(
		"starting execution",
		"test_id", assignment.TestID,
	)

	select {

	case <-ctx.Done():
		return ctx.Err()

	case <-time.After(10 * time.Second):
	}

	w.logger.Info(
		"execution completed",
		"test_id", assignment.TestID,
	)

	return nil
}