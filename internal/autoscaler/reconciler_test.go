package autoscaler

import (
	"context"
	"vulcan/internal/models"
)

type fakeTestRepository struct {
	tests []models.Test
	err   error
}

func (f *fakeTestRepository) GetTests(ctx context.Context) ([]models.Test, error) {
	return f.tests, f.err
}

func (f *fakeTestRepository) CreateTest(context.Context, *models.Test) error {
	return nil
}

func (f *fakeTestRepository) GetTestByID(context.Context, string) (*models.Test, error) {
	return nil, nil
}

func (f *fakeTestRepository) UpdateStatus(context.Context, string, models.TestStatus) error {
	return nil
}

func (f *fakeTestRepository) UpdateWorkerCount(context.Context, string, int) error {
	return nil
}

func (f *fakeTestRepository) TryTransitionStatus(
	context.Context,
	string,
	models.TestStatus,
	models.TestStatus,
) (bool, error) {
	return true, nil
}

type fakeWorkerRepository struct {
	workers []models.Worker

	terminateIDs []string
	terminateErr error
}

func (f *fakeWorkerRepository) GetWorkers(context.Context) ([]models.Worker, error) {
	return f.workers, nil
}

func (f *fakeWorkerRepository) TerminateIdleWorkers(
	ctx context.Context,
	workerIDs []string,
) ([]string, error) {
	if f.terminateErr != nil {
		return nil, f.terminateErr
	}

	f.terminateIDs = append([]string(nil), workerIDs...)

	return workerIDs, nil
}

func (f *fakeWorkerRepository) CreateWorker(context.Context, *models.Worker) error {
	return nil
}

func (f *fakeWorkerRepository) GetWorkerByID(context.Context, string) (*models.Worker, error) {
	return nil, nil
}

func (f *fakeWorkerRepository) UpdateHeartbeat(
	context.Context,
	string,
	models.WorkerStatus,
) error {
	return nil
}

func (f *fakeWorkerRepository) MarkOfflineWorkers(
	ctx context.Context,
	_ interface{},
) (int64, error) {
	return 0, nil
}

type fakeProvisioner struct {
	provisionCount int
	terminateIDs   []string

	provisionErr  error
	terminateErr  error
}

func (f *fakeProvisioner) Provision(
	_ context.Context,
	count int,
) error {
	f.provisionCount += count
	return f.provisionErr
}

func (f *fakeProvisioner) Terminate(
	_ context.Context,
	hostnames []string,
) error {
	f.terminateIDs = append([]string(nil), hostnames...)
	return f.terminateErr
}