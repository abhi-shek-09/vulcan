package execution

import (
	"context"
	"vulcan/internal/api/apierrors"
)

type Engine interface {
	Run(
		ctx context.Context,
		cfg ExecutionConfig,
		record func(Result),
	) error
}

type DefaultEngine struct {
	scheduler Scheduler
	executor  Executor
}

func NewEngine(
	scheduler Scheduler,
	executor Executor,
) *DefaultEngine {
	return &DefaultEngine{
		scheduler: scheduler,
		executor:  executor,
	}
}

func (e *DefaultEngine) Run(
	ctx context.Context,
	cfg ExecutionConfig,
	record func(Result),
) error {
	if e.scheduler == nil {
		return apierrors.ErrNilScheduler
	}

	if e.executor == nil {
		return apierrors.ErrNilExecutor
	}

	return e.scheduler.Run(
		ctx,
		cfg,
		func(ctx context.Context) Result {
			return e.executor.Execute(ctx, cfg)
		},
		record,
	)
}
