package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"vulcan/internal/models"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type RegisterWorkerRequest struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`

	CPUCount int `json:"cpu_count"`
	MemoryMB int `json:"memory_mb"`
}

type RegisterWorkerResponse struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}


type AssignmentResponse struct {
	Assigned bool `json:"assigned"`
	TestID string `json:"test_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type HeartbeatResponse struct {
	Status string `json:"status"`
}

type HeartbeatRequest struct {
	Status models.WorkerStatus `json:"status"`
}

func (c *Client) doRequest(ctx context.Context, method string, path string, request any,response any,expectedStatus int) error {

	var body io.Reader
	if request != nil {
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = bytes.NewBuffer(payload)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		body,
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf(
			"%s %s failed (%d): %s",
			method,
			path,
			resp.StatusCode,
			string(data),
		)
	}

	if response == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(response)
}

func (c *Client) RegisterWorker(ctx context.Context,req RegisterWorkerRequest) (*RegisterWorkerResponse, error) {

	var resp RegisterWorkerResponse

	err := c.doRequest(
		ctx,
		http.MethodPost,
		"/api/v1/workers",
		req,
		&resp,
		http.StatusCreated,
	)

	if err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) Heartbeat(
	ctx context.Context,
	workerID string,
	status models.WorkerStatus,
) error {

	req := HeartbeatRequest{
		Status: status,
	}

	return c.doRequest(
		ctx,
		http.MethodPost,
		fmt.Sprintf("/api/v1/workers/%s/heartbeat", workerID),
		req,
		nil,
		http.StatusNoContent,
	)
}

func (c *Client) GetAssignment(ctx context.Context, workerID string, ) (*AssignmentResponse, error) {

	var assignment AssignmentResponse
	err := c.doRequest(
		ctx,
		http.MethodGet,
		fmt.Sprintf("/api/v1/workers/%s/assignment", workerID),
		nil,
		&assignment,
		http.StatusOK,
	)
	if err != nil {
		return nil, err
	}

	return &assignment, nil
}

func (c *Client) StartAssignment(ctx context.Context, workerID string, testID string,) error {

	req := models.AssignmentTransitionRequest{
		TestID: testID,
	}

	return c.doRequest(
		ctx,
		http.MethodPost,
		fmt.Sprintf("/api/v1/workers/%s/assignment/start", workerID),
		req,
		nil,
		http.StatusNoContent,
	)
}

func (c *Client) CompleteAssignment(ctx context.Context, workerID string, testID string) error {

	req := models.AssignmentTransitionRequest{
		TestID: testID,
	}

	return c.doRequest(
		ctx,
		http.MethodPost,
		fmt.Sprintf("/api/v1/workers/%s/assignment/complete", workerID),
		req,
		nil,
		http.StatusNoContent,
	)
}