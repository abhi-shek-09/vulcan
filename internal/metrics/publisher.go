package metrics

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

type Publisher struct {
	conn *nats.Conn
}

func NewPublisher(url string) (*Publisher, error) {

	nc, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		conn: nc,
	}, nil
}

func (p *Publisher) Close() {
	if p.conn != nil {
		p.conn.Close()
	}
}

func (p *Publisher) Publish(ctx context.Context, bucket MetricBucket) error {

	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}

	subject := fmt.Sprintf("metrics.test.%s", bucket.TestID)

	return p.conn.Publish(subject, data)
}
