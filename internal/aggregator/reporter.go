package aggregator

import (
	"context"
	"time"
)

func (a *Aggregator) reportLoop(ctx context.Context) {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:

			a.report()
		}
	}
}