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

type TestHandler struct {
	service *service.TestService
}

func NewTestHandler(service *service.TestService) *TestHandler {
	return &TestHandler{
		service: service,
	}
}

func (h *TestHandler) CreateTest(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTestRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, apierrors.ValidationError, err.Error())
		return
	}

	test, err := h.service.CreateTest(r.Context(), req)
	if err != nil {
		response.Error(w, http.StatusBadRequest, apierrors.ValidationError, err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, test)
}

func (h *TestHandler) GetTests(w http.ResponseWriter, r *http.Request) {
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

func (h *TestHandler) GetTestByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	test, err := h.service.GetTestByID(r.Context(), id)
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

func (h *TestHandler) StopTest(w http.ResponseWriter, r *http.Request) {

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
