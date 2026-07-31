package worker

import (
	"context"
	"time"
)

func (w *Worker) HeartbeatLoop(ctx context.Context) {

	ticker := time.NewTicker(w.config.Heartbeat.Interval)
	defer ticker.Stop()

	w.logger.Info(
		"heartbeat loop started",
		"interval", w.config.Heartbeat.Interval,
	)

	for {
		select {

		case <-ctx.Done():
			w.logger.Info("heartbeat loop stopped")
			return

		case <-ticker.C:

			if err := w.sendHeartbeat(ctx); err != nil {

				w.logger.Error(
					"heartbeat failed",
					"error", err,
				)

				continue
			}

			w.logger.Debug("heartbeat sent")
		}
	}
}
