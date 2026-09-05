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

// PendingCapacity calculates the number of workers required by tests that
// have been created but not yet started (status == CREATED).
//
// This is deliberately kept separate from DesiredCapacity: DesiredCapacity
// answers "how much capacity do currently active tests need" and is used to
// decide how much to *provision* (scale up). PendingCapacity answers "how
// much capacity has already been earmarked for tests that are about to
// start" and is used only to decide how much *existing* idle capacity is
// safe to drain (scale down).
//
// The distinction matters: a CREATED test has not claimed any workers yet
// (that only happens via StartTest's reservation), so it should not by
// itself trigger new provisioning ahead of time. But an idle worker that a
// pending test is about to need must not be pulled out from under it by the
// periodic scale-down pass -- otherwise the fleet can drain to zero between
// "test created" and "test started" and the test is left stranded in
// CREATED with no worker ever available to claim it.
func PendingCapacity(tests []models.Test, workerCapacityRPS int) (int, error) {
	pending := 0

	for _, test := range tests {
		if test.Status != models.StatusCreated {
			continue
		}

		required, err := scheduler.RequiredWorkers(
			test.RPS,
			workerCapacityRPS,
		)
		if err != nil {
			return 0, fmt.Errorf(
				"calculate pending workers for test %s: %w",
				test.ID,
				err,
			)
		}

		pending += required
	}

	return pending, nil
}
