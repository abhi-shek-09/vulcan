package autoscaler

import (
	"fmt"

	"vulcan/internal/models"
	"vulcan/internal/scheduler"
)

// DesiredCapacity calculates the total number of workers required by currently active tests.
//
// A test contributes capacity while it is STARTING or RUNNING. Worker requirements are calculated using the existing scheduler
// capacity logic so the autoscaler does not maintain a second capacity calculation system.
func DesiredCapacity(tests []models.Test, workerCapacityRPS int) (int, error) {
	desired := 0

	for _, test := range tests {
		if !isActiveTest(test.Status) {
			continue
		}

		required, err := scheduler.RequiredWorkers(
			test.RPS,
			workerCapacityRPS,
		)
		if err != nil {
			return 0, fmt.Errorf(
				"calculate workers for test %s: %w",
				test.ID,
				err,
			)
		}

		desired += required
	}

	return desired, nil
}

func isActiveTest(status models.TestStatus) bool {
	return status == models.StatusStarting ||
		status == models.StatusRunning
}
