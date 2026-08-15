package execution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPExecutorGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	executor := NewHTTPExecutor(5 * time.Second)

	result := executor.Execute(
		context.Background(),
		ExecutionConfig{
			TargetURL: server.URL,
			Method:    http.MethodGet,
		},
	)

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}

	if !result.Success {
		t.Fatal("expected successful result")
	}

	if result.BytesReceived != 5 {
		t.Fatalf("expected 5 response bytes, got %d", result.BytesReceived)
	}

	if result.Latency <= 0 {
		t.Fatal("expected positive latency")
	}
}

func TestHTTPExecutorPOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("X-Test") != "vulcan" {
			t.Fatalf("expected X-Test header")
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	body := []byte(`{"hello":"world"}`)

	executor := NewHTTPExecutor(5 * time.Second)

	result := executor.Execute(
		context.Background(),
		ExecutionConfig{
			TargetURL: server.URL,
			Method:    http.MethodPost,
			Headers: map[string]string{
				"X-Test": "vulcan",
			},
			Body: body,
		},
	)

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	if result.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", result.StatusCode)
	}

	if !result.Success {
		t.Fatal("expected successful result")
	}

	if result.BytesSent != int64(len(body)) {
		t.Fatalf(
			"expected %d request bytes, got %d",
			len(body),
			result.BytesSent,
		)
	}
}

func TestHTTPExecutorHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	executor := NewHTTPExecutor(5 * time.Second)

	result := executor.Execute(
		context.Background(),
		ExecutionConfig{
			TargetURL: server.URL,
			Method:    http.MethodGet,
		},
	)

	if result.Err != nil {
		t.Fatalf("HTTP 400 should not produce transport error: %v", result.Err)
	}

	if result.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", result.StatusCode)
	}

	if result.Success {
		t.Fatal("expected unsuccessful result")
	}

	if result.BytesReceived == 0 {
		t.Fatal("expected response bytes")
	}
}

func TestHTTPExecutorConnectionFailure(t *testing.T) {
	executor := NewHTTPExecutor(1 * time.Second)

	result := executor.Execute(
		context.Background(),
		ExecutionConfig{
			TargetURL: "http://127.0.0.1:1",
			Method:    http.MethodGet,
		},
	)

	if result.Err == nil {
		t.Fatal("expected connection error")
	}

	if result.Success {
		t.Fatal("expected unsuccessful result")
	}
}

func TestHTTPExecutorCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	executor := NewHTTPExecutor(10 * time.Second)

	done := make(chan Result, 1)

	go func() {
		done <- executor.Execute(
			ctx,
			ExecutionConfig{
				TargetURL: server.URL,
				Method:    http.MethodGet,
			},
		)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	result := <-done

	if result.Err == nil {
		t.Fatal("expected cancellation error")
	}

	if result.Success {
		t.Fatal("expected unsuccessful result")
	}
}
