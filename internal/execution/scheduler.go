package execution

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidRPS         = errors.New("rps must be greater than zero")
	ErrInvalidConcurrency = errors.New("concurrency must be greater than zero")
	ErrInvalidDuration    = errors.New("duration must be greater than zero")
)

type Scheduler interface {
	Run(
		ctx context.Context,
		cfg ExecutionConfig,
		execute func(context.Context) Result,
		record func(Result),
	) error
}

type FixedRateScheduler struct{}

func NewScheduler() *FixedRateScheduler {
	return &FixedRateScheduler{}
}

func (s *FixedRateScheduler) Run(
	ctx context.Context,
	cfg ExecutionConfig,
	execute func(context.Context) Result,
	record func(Result),
) error {
	if cfg.RPS <= 0 {
		return ErrInvalidRPS
	}

	if cfg.Concurrency <= 0 {
		return ErrInvalidConcurrency
	}

	if cfg.Duration <= 0 {
		return ErrInvalidDuration
	}

	if execute == nil {
		return errors.New("execute function is nil")
	}

	if record == nil {
		return errors.New("record function is nil")
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	interval := time.Second / time.Duration(cfg.RPS)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sem := make(chan struct{}, cfg.Concurrency)

	var wg sync.WaitGroup

	for {
		select {
		case <-runCtx.Done():
			wg.Wait()
			return nil

		case <-ticker.C:
			// Never block the scheduler waiting for concurrency.
			select {
			case sem <- struct{}{}:
				wg.Add(1)

				go func() {
					defer wg.Done()
					defer func() {
						<-sem
					}()

					result := execute(runCtx)
					record(result)
				}()

			default:
				// Concurrency is currently exhausted.
				// This pacing slot is intentionally dropped.
			}
		}
	}
}
