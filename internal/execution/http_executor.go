package execution

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

type HTTPExecutor struct {
	client *http.Client
}

func NewHTTPExecutor(timeout time.Duration) *HTTPExecutor {
	transport := &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
		ForceAttemptHTTP2:   true,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return &HTTPExecutor{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (e *HTTPExecutor) Execute(
	ctx context.Context,
	cfg ExecutionConfig,
) Result {
	start := time.Now()

	req, err := http.NewRequestWithContext(
		ctx,
		cfg.Method,
		cfg.TargetURL,
		bytes.NewReader(cfg.Body),
	)
	if err != nil {
		return Result{
			Latency: time.Since(start),
			Success: false,
			Err:     err,
		}
	}

	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	bytesSent := int64(len(cfg.Body))

	resp, err := e.client.Do(req)
	if err != nil {
		return Result{
			Latency:       time.Since(start),
			Success:       false,
			BytesSent:     bytesSent,
			BytesReceived: 0,
			Err:           err,
		}
	}

	defer resp.Body.Close()

	bytesReceived, readErr := io.Copy(io.Discard, resp.Body)

	latency := time.Since(start)

	if readErr != nil {
		return Result{
			StatusCode:    resp.StatusCode,
			Latency:       latency,
			Success:       false,
			BytesSent:     bytesSent,
			BytesReceived: bytesReceived,
			Err:           readErr,
		}
	}

	return Result{
		StatusCode:    resp.StatusCode,
		Latency:       latency,
		Success:       resp.StatusCode >= 200 && resp.StatusCode < 400,
		BytesSent:     bytesSent,
		BytesReceived: bytesReceived,
	}
}
