package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"vulcan/internal/api/handlers"
	"vulcan/internal/api/router"
	"vulcan/internal/config"
	"vulcan/internal/db"
	"vulcan/internal/reconciler"
	"vulcan/internal/repository"
	"vulcan/internal/scheduler"
	"vulcan/internal/service"
)

func main() {
	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize database
	pool, err := db.New(cfg)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Dependency Injection

	workerRepository := repository.NewWorkerRepository(pool)
	workerService := service.NewWorkerService(workerRepository)
	workerHandler := handlers.NewWorkerHandler(workerService)

	schedulerSvc := scheduler.NewDefaultScheduler(workerRepository)
	testRepository := repository.NewTestRepository(pool)
	testService := service.NewTestService(testRepository, schedulerSvc, workerRepository)
	testHandler := handlers.NewTestHandler(testService)


	workerReconciler := reconciler.NewWorkerReconciler(workerRepository, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go workerReconciler.Start(ctx)

	// Initialize router
	r := api.NewRouter(testHandler, workerHandler)

	logger.Info(
		"starting control plane",
		"port", cfg.Port,
	)

	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
