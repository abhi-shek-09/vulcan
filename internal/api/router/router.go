package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"vulcan/internal/api/handlers"
	"vulcan/internal/api/response"
)

func NewRouter(testHandler *handlers.TestHandler) *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	// r.Use(middleware.RealIP) this is vulnerable to spoofing so do not add this
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health
	r.Get("/health", healthHandler)

	// API
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/tests", testHandler.CreateTest)
		r.Get("/tests", testHandler.GetTests)
		r.Get("/tests/{id}", testHandler.GetTestByID)
		r.Post("/tests/{id}/stop", testHandler.StopTest)
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	}))
}
