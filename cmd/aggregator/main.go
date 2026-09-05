package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vulcan/internal/aggregator"
	"vulcan/internal/config"
)

func main() {

	logger := slog.New(
		slog.NewTextHandler(
			os.Stdout,
			&slog.HandlerOptions{
				Level: slog.LevelInfo,
			},
		),
	)

	cfg, err := config.LoadAggregatorConfig()
	if err != nil {
		log.Fatal(err)
	}

	agg, err := aggregator.New(cfg, logger)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := agg.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
