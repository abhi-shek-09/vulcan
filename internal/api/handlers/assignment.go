package handlers

import (
	"encoding/json"
	"net/http"
	"vulcan/internal/api/response"
	"vulcan/internal/controlplane"
	"vulcan/internal/models"

	"github.com/go-chi/chi/v5"
)

func (h *WorkerHandler) GetWorkerAssignment(
	w http.ResponseWriter,
	r *http.Request,
) {

	workerID := chi.URLParam(r, "id")
	assignment, err := h.service.GetReservedAssignment(
		r.Context(),
		workerID,
	)
	if err != nil {
		response.Error(
			w,
			http.StatusInternalServerError,
			"INTERNAL_SERVER_ERROR",
			err.Error(),
		)
		return
	}

	if assignment == nil {
		response.JSON(
			w,
			http.StatusOK,
			controlplane.AssignmentResponse{
				Assigned: false,
			},
		)
		return
	}

	response.JSON(
		w,
		http.StatusOK,
		controlplane.AssignmentResponse{
			Assigned:    true,
			TestID:      assignment.TestID,
			Status:      string(assignment.Status),
			TargetURL:   assignment.TargetURL,
			Method:      assignment.Method,
			DurationSec: assignment.DurationSec,
			RPS:         assignment.RPS,
			Concurrency: assignment.Concurrency,
		},
	)
}

func (h *WorkerHandler) StartAssignment(w http.ResponseWriter, r *http.Request) {

	workerID := chi.URLParam(r, "id")

	var req models.AssignmentTransitionRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid request body",
		)
		return
	}

	if err := h.service.StartAssignment(
		r.Context(),
		req.TestID,
		workerID,
	); err != nil {

		response.Error(
			w,
			http.StatusInternalServerError,
			"INTERNAL_SERVER_ERROR",
			err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkerHandler) CompleteAssignment(
	w http.ResponseWriter,
	r *http.Request,
) {

	workerID := chi.URLParam(r, "id")

	var req models.AssignmentTransitionRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {

		response.Error(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid request body",
		)
		return
	}

	if err := h.service.CompleteAssignment(
		r.Context(),
		req.TestID,
		workerID,
	); err != nil {

		response.Error(
			w,
			http.StatusInternalServerError,
			"INTERNAL_SERVER_ERROR",
			err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkerHandler) FailAssignment(w http.ResponseWriter, r *http.Request) {
	workerID := chi.URLParam(r, "id")

	var req models.AssignmentTransitionRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid request body",
		)
		return
	}

	if err := h.service.FailAssignment(
		r.Context(),
		req.TestID,
		workerID,
	); err != nil {
		response.Error(
			w,
			http.StatusInternalServerError,
			"INTERNAL_SERVER_ERROR",
			err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
