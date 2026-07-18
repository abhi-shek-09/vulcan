package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"vulcan/internal/api/handlers"
	"vulcan/internal/api/response"
)

func NewRouter(testHandler *handlers.TestHandler, workerHandler *handlers.WorkerHandler) *chi.Mux {
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
		r.Route("/workers", func(r chi.Router) {
			r.Post("/", workerHandler.RegisterWorker)
			r.Get("/", workerHandler.GetWorkers)
			r.Get("/{id}", workerHandler.GetWorkerByID)
			r.Post("/{id}/heartbeat",workerHandler.Heartbeat,)
		})

		r.Route("/tests", func(r chi.Router) {
			r.Post("/", testHandler.CreateTest)
			r.Get("/", testHandler.GetTests)
			r.Get("/{id}", testHandler.GetTestByID)
			r.Post("/{id}/start",testHandler.StartTest)
			r.Post("/{id}/stop", testHandler.StopTest)
		})

	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    payload := map[string]string{"status": "ok"}
    response.JSON(w, http.StatusOK, payload)
}