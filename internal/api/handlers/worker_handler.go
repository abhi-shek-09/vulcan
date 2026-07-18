package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/api/response"
	"vulcan/internal/service"
)

type WorkerHandler struct {
	service *service.WorkerService
}

func NewWorkerHandler(service *service.WorkerService) *WorkerHandler {
	return &WorkerHandler{
		service: service,
	}
}

func (h *WorkerHandler) RegisterWorker(w http.ResponseWriter, r *http.Request) {

	var req service.RegisterWorkerRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			apierrors.ValidationError,
			err.Error(),
		)
		return
	}

	worker, err := h.service.RegisterWorker(r.Context(), req)
	if err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			apierrors.ValidationError,
			err.Error(),
		)
		return
	}

	response.JSON(
		w,
		http.StatusCreated,
		worker,
	)
}

func (h *WorkerHandler) GetWorkers(w http.ResponseWriter, r *http.Request) {

	workers, err := h.service.GetWorkers(r.Context())
	if err != nil {
		response.Error(
			w,
			http.StatusInternalServerError,
			apierrors.InternalError,
			"failed to fetch workers",
		)
		return
	}

	response.JSON(
		w,
		http.StatusOK,
		workers,
	)
}

func (h *WorkerHandler) GetWorkerByID(w http.ResponseWriter, r *http.Request) {

	id := chi.URLParam(r, "id")

	worker, err := h.service.GetWorkerByID(
		r.Context(),
		id,
	)

	if err != nil {

		if errors.Is(err, apierrors.ErrWorkerNotFound) {
			response.Error(
				w,
				http.StatusNotFound,
				apierrors.NotFoundError,
				err.Error(),
			)
			return
		}

		response.Error(
			w,
			http.StatusInternalServerError,
			apierrors.InternalError,
			"internal server error",
		)
		return
	}

	response.JSON(
		w,
		http.StatusOK,
		worker,
	)
}

func (h *WorkerHandler) Heartbeat(
	w http.ResponseWriter,
	r *http.Request,
) {

	id := chi.URLParam(r, "id")

	var req service.HeartbeatRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			apierrors.ValidationError,
			err.Error(),
		)
		return
	}

	err := h.service.Heartbeat(
		r.Context(),
		id,
		req,
	)

	if err != nil {

		if errors.Is(err, apierrors.ErrWorkerNotFound) {
			response.Error(
				w,
				http.StatusNotFound,
				apierrors.NotFoundError,
				err.Error(),
			)
			return
		}

		response.Error(
			w,
			http.StatusBadRequest,
			apierrors.ValidationError,
			err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}