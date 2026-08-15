package execution

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRespectsDuration(t *testing.T) {
	scheduler := NewScheduler()

	var requests atomic.Int64

	start := time.Now()

	err := scheduler.Run(
		context.Background(),
		ExecutionConfig{
			Duration:    250 * time.Millisecond,
			RPS:         20,
			Concurrency: 5,
		},
		func(ctx context.Context) Result {
			requests.Add(1)
			return Result{
				Success: true,
			}
		},
		func(result Result) {},
	)

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if elapsed < 250*time.Millisecond {
		t.Fatalf("scheduler returned too early: %v", elapsed)
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("scheduler took too long: %v", elapsed)
	}

	if requests.Load() == 0 {
		t.Fatal("expected requests to be executed")
	}
}

func TestSchedulerRespectsConcurrency(t *testing.T) {
	scheduler := NewScheduler()

	var active atomic.Int64
	var maximum atomic.Int64

	err := scheduler.Run(
		context.Background(),
		ExecutionConfig{
			Duration:    200 * time.Millisecond,
			RPS:         100,
			Concurrency: 3,
		},
		func(ctx context.Context) Result {
			current := active.Add(1)

			for {
				old := maximum.Load()

				if current <= old {
					break
				}

				if maximum.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)

			active.Add(-1)

			return Result{
				Success: true,
			}
		},
		func(result Result) {},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if maximum.Load() > 3 {
		t.Fatalf(
			"maximum concurrency exceeded: got %d",
			maximum.Load(),
		)
	}
}

func TestSchedulerCancellation(t *testing.T) {
	scheduler := NewScheduler()

	ctx, cancel := context.WithCancel(context.Background())

	var requests atomic.Int64

	done := make(chan error, 1)

	go func() {
		done <- scheduler.Run(
			ctx,
			ExecutionConfig{
				Duration:    10 * time.Second,
				RPS:         100,
				Concurrency: 10,
			},
			func(ctx context.Context) Result {
				requests.Add(1)

				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}

				return Result{
					Success: true,
				}
			},
			func(result Result) {},
		)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestSchedulerValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  ExecutionConfig
	}{
		{
			name: "invalid rps",
			cfg: ExecutionConfig{
				Duration:    time.Second,
				RPS:         0,
				Concurrency: 1,
			},
		},
		{
			name: "invalid concurrency",
			cfg: ExecutionConfig{
				Duration:    time.Second,
				RPS:         1,
				Concurrency: 0,
			},
		},
		{
			name: "invalid duration",
			cfg: ExecutionConfig{
				Duration:    0,
				RPS:         1,
				Concurrency: 1,
			},
		},
	}

	scheduler := NewScheduler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := scheduler.Run(
				context.Background(),
				tt.cfg,
				func(ctx context.Context) Result {
					return Result{}
				},
				func(result Result) {},
			)

			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
