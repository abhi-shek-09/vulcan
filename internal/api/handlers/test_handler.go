package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/api/response"
	"vulcan/internal/models"
	"encoding/json"
	"vulcan/internal/service"
)

type TestHandler struct {
	service *service.TestService
}

type StartTestResponse struct {
	TestID  string          `json:"test_id"`
	Status  string          `json:"status"`
	Workers []WorkerSummary `json:"workers"`
}

type WorkerSummary struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

func NewTestHandler(service *service.TestService) *TestHandler {
	return &TestHandler{
		service: service,
	}
}

func (h *TestHandler) CreateTest(
	w http.ResponseWriter,
	r *http.Request,
) {

	var req service.CreateTestRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(
			w,
			http.StatusBadRequest,
			apierrors.ValidationError,
			err.Error(),
		)
		return
	}

	test, err := h.service.CreateTest(
		r.Context(),
		req,
	)

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
		test,
	)
}

func (h *TestHandler) GetTests(
	w http.ResponseWriter,
	r *http.Request,
) {

	tests, err := h.service.GetTests(r.Context())
	if err != nil {
		response.Error(
			w,
			http.StatusInternalServerError,
			apierrors.InternalError,
			"failed to fetch tests",
		)
		return
	}

	response.JSON(
		w,
		http.StatusOK,
		tests,
	)
}

func (h *TestHandler) GetTestByID(
	w http.ResponseWriter,
	r *http.Request,
) {

	id := chi.URLParam(r, "id")

	test, err := h.service.GetTestByID(
		r.Context(),
		id,
	)

	if err != nil {

		if errors.Is(err, apierrors.ErrTestNotFound) {
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
		test,
	)
}

func (h *TestHandler) StopTest(
	w http.ResponseWriter,
	r *http.Request,
) {

	id := chi.URLParam(r, "id")

	err := h.service.StopTest(
		r.Context(),
		id,
	)

	if err != nil {

		if errors.Is(err, apierrors.ErrTestNotFound) {
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

	w.WriteHeader(http.StatusNoContent)
}

func (h *TestHandler) StartTest(
	w http.ResponseWriter,
	r *http.Request,
) {

	testID := chi.URLParam(r, "id")

	workers, err := h.service.StartTest(
		r.Context(),
		testID,
	)

	if err != nil {

		switch {

		case errors.Is(err, apierrors.ErrTestNotFound):
			response.Error(
				w,
				http.StatusNotFound,
				apierrors.NotFoundError,
				err.Error(),
			)

		case errors.Is(err, apierrors.ErrInvalidTestState):
			response.Error(
				w,
				http.StatusConflict,
				apierrors.ValidationError,
				err.Error(),
			)

		case errors.Is(err, apierrors.ErrInsufficientWorkers):
			response.Error(
				w,
				http.StatusConflict,
				apierrors.ValidationError,
				err.Error(),
			)

		default:
			response.Error(
				w,
				http.StatusInternalServerError,
				apierrors.InternalError,
				"internal server error",
			)
		}

		return
	}

	resp := StartTestResponse{
		TestID: testID,
		Status: string(models.StatusRunning),
	}

	for _, worker := range workers {

		resp.Workers = append(
			resp.Workers,
			WorkerSummary{
				ID:       worker.ID,
				Hostname: worker.Hostname,
			},
		)
	}

	response.JSON(
		w,
		http.StatusOK,
		resp,
	)
}