package worker

import (
	"context"
	"time"
)

func (w *Worker) PollLoop(ctx context.Context) {

	ticker := time.NewTicker(w.config.Polling.Interval)
	defer ticker.Stop()

	w.logger.Info(
		"poll loop started",
		"interval", w.config.Polling.Interval,
	)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("poll loop stopped")
			return

		case <-ticker.C:
			if err := w.poll(ctx); err != nil {
				w.logger.Error(
					"poll failed",
					"error", err,
				)
			}
		}
	}
}