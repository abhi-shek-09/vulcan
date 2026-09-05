package victoria

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

type Writer struct {
	url    string
	client *http.Client
}

func New(url string) *Writer {
	return &Writer{
		url:    url,
		client: &http.Client{},
	}
}

func (w *Writer) Write(ctx context.Context, body string) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		w.url+"/api/v1/import/prometheus",
		bytes.NewBufferString(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set(
		"Content-Type",
		"text/plain",
	)

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf(
			"victoria returned %s",
			resp.Status,
		)
	}

	return nil
}
