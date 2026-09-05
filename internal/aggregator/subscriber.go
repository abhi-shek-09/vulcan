package aggregator

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"vulcan/internal/metrics"
)

func (a *Aggregator) subscribe(ctx context.Context) error {
	_, err := a.conn.Subscribe(
		"metrics.test.*",
		func(msg *nats.Msg) {
			var bucket metrics.MetricBucket
			if err := json.Unmarshal(msg.Data, &bucket); err != nil {

				a.logger.Error(
					"failed to decode metric bucket",
					"error", err,
				)
				return
			}

			window := a.store.Get(bucket.TestID)
			window.Merge(bucket)
		},
	)
	if err != nil {
		return err
	}
	<-ctx.Done()

	return nil
}
