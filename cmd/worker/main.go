package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"vulcan/internal/config"
	"vulcan/internal/metrics"
	"vulcan/internal/worker"
)

func main() {
	cfg, err := worker.LoadConfig("configs/worker.yaml")
	if err != nil {
		panic(err)
	}
	var level slog.Level

	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	).With(
		"worker", cfg.Worker.Hostname,
		"version", cfg.Worker.Version,
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	defer cancel()

	envCfg, err := config.LoadWorkerConfig()
	if err != nil {
		log.Fatal(err)
	}

	publisher, err := metrics.NewPublisher(envCfg.NATSURL)
	if err != nil {
		logger.Error(
			"failed to connect to NATS",
			"error", err,
		)
		os.Exit(1)
	}

	defer publisher.Close()
	w := worker.New(cfg, logger, publisher)

	runtime := worker.NewRuntime(
		logger,
		w,
	)

	if err := runtime.Start(ctx); err != nil {
		logger.Error(
			"worker exited",
			"error",
			err,
		)
		os.Exit(1)
	}
}
