// Package kafkapublisher implements port.Publisher on Kafka.
package kafkapublisher

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/b1ll1eee/vindice-observability/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

type Publisher struct{ cl *kgo.Client }

var _ port.Publisher = (*Publisher)(nil)

func New(brokers []string) (*Publisher, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ProducerBatchCompression(kgo.ZstdCompression()),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.ProducerBatchMaxBytes(kafka.MaxMessageBytes),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		return nil, err
	}
	return &Publisher{cl: cl}, nil
}

// Publish blocks until Kafka has acknowledged the record, so a success
// response to the OTLP client means the data is durable.
func (p *Publisher) Publish(ctx context.Context, sig domain.Signal, payload []byte) error {
	topic, ok := kafka.Topic(sig)
	if !ok {
		return fmt.Errorf("%w: %q", domain.ErrInvalidSignal, sig)
	}
	return p.cl.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: payload}).FirstErr()
}

func (p *Publisher) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = p.cl.Flush(ctx)
	p.cl.Close()
}
