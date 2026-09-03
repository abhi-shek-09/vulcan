package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"vulcan/internal/api/handlers"
	"vulcan/internal/api/router"
	"vulcan/internal/autoscaler"
	"vulcan/internal/config"
	"vulcan/internal/dashboard"
	"vulcan/internal/db"
	"vulcan/internal/provisioner"
	"vulcan/internal/reconciler"
	"vulcan/internal/repository"
	"vulcan/internal/scheduler"
	"vulcan/internal/service"
)

func main() {
	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Load configuration
	cfg, err := config.LoadServerConfig()
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
	workerProvisioner := provisioner.NewProcessProvisioner(
		cfg.WorkerBinaryPath,
		cfg.WorkerWorkingDir,
		cfg.NATSURL,
		cfg.ControlPlaneURL,
	)

	// The autoscaler owns fleet-capacity decisions: it periodically
	// reconciles the worker fleet against currently active tests, and
	// TestService also asks it to reconcile immediately when a test start
	// needs more workers than are currently idle. Routing both paths
	// through the same Reconciler (which serializes itself internally)
	// keeps provisioning/termination as a single, coherent policy instead
	// of Phase 8's test-start provisioning racing the autoscaler.
	fleetReconciler := autoscaler.NewReconciler(
		testRepository,
		workerRepository,
		workerProvisioner,
		cfg.WorkerCapacityRPS,
		logger,
	)

	testService := service.NewTestService(
		testRepository,
		schedulerSvc,
		workerRepository,
		workerProvisioner,
		cfg.WorkerCapacityRPS,
		fleetReconciler,
	)
	testHandler := handlers.NewTestHandler(testService)

	workerReconciler := reconciler.NewWorkerReconciler(workerRepository, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go workerReconciler.Start(ctx)

	go func() {
		if err := fleetReconciler.Run(ctx, cfg.AutoscalerInterval); err != nil && ctx.Err() == nil {
			logger.Error("worker fleet autoscaler stopped unexpectedly", "error", err)
		}
	}()

	victoriaClient := dashboard.NewClient(
		cfg.VictoriaMetricsURL,
	)
	logger.Info(
		"victoria metrics url",
		"url",
		cfg.VictoriaMetricsURL,
	)
	dashboardService := dashboard.NewService(
		victoriaClient,
	)

	dashboardHandler := handlers.NewDashboardHandler(
		dashboardService,
	)

	// Initialize router
	r := api.NewRouter(testHandler, workerHandler, dashboardHandler)

	logger.Info(
		"starting control plane",
		"port", cfg.Port,
	)

	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
