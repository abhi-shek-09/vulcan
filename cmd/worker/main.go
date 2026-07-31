package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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

	// logger := slog.New(
	// 	slog.NewTextHandler(os.Stdout, opts),
	// )
	
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

	w := worker.New(cfg, logger)

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