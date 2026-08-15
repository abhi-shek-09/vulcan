package execution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type fakeExecutor struct {
	calls atomic.Int64
}

func (f *fakeExecutor) Execute(
	ctx context.Context,
	cfg ExecutionConfig,
) Result {
	f.calls.Add(1)

	return Result{
		StatusCode: 200,
		Success:    true,
	}
}

func TestEngineRunsExecutor(t *testing.T) {
	executor := &fakeExecutor{}

	engine := NewEngine(
		NewScheduler(),
		executor,
	)

	var results atomic.Int64

	err := engine.Run(
		context.Background(),
		ExecutionConfig{
			Duration:    200 * time.Millisecond,
			RPS:         20,
			Concurrency: 5,
		},
		func(result Result) {
			results.Add(1)
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if executor.calls.Load() == 0 {
		t.Fatal("expected executor to be called")
	}

	if results.Load() == 0 {
		t.Fatal("expected results to be recorded")
	}

	if executor.calls.Load() != results.Load() {
		t.Fatalf(
			"executor calls (%d) != results (%d)",
			executor.calls.Load(),
			results.Load(),
		)
	}
}

func TestEngineSendsRealHTTPRequests(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)

			w.WriteHeader(http.StatusOK)

			_, _ = w.Write([]byte("ok"))
		},
	))
	defer server.Close()

	executor := NewHTTPExecutor(5 * time.Second)

	engine := NewEngine(
		NewScheduler(),
		executor,
	)

	var results atomic.Int64

	err := engine.Run(
		context.Background(),
		ExecutionConfig{
			TargetURL:   server.URL,
			Method:      http.MethodGet,
			Duration:    250 * time.Millisecond,
			RPS:         20,
			Concurrency: 5,
		},
		func(result Result) {
			results.Add(1)

			if result.Err != nil {
				return
			}

			if result.StatusCode != http.StatusOK {
				t.Errorf(
					"expected 200, got %d",
					result.StatusCode,
				)
			}
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requests.Load() == 0 {
		t.Fatal("expected target to receive requests")
	}

	if results.Load() == 0 {
		t.Fatal("expected engine to produce results")
	}

	if requests.Load() > results.Load() {
		t.Fatalf(
			"target received %d requests but engine recorded only %d results",
			requests.Load(),
			results.Load(),
		)
	}
}
