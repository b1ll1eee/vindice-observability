// Package kafka holds the Kafka settings shared by the publisher and consumer
// adapters. Kafka buffers raw OTLP protobuf export requests (zstd-compressed)
// between ingest and the ClickHouse writer, so ClickHouse can be slow or down
// without losing data.
package kafka

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// MaxMessageBytes must stay at or below the broker's message.max.bytes.
const MaxMessageBytes = 20 << 20

var topics = map[domain.Signal]string{
	domain.SignalLogs:    "vindice.otlp.logs",
	domain.SignalTraces:  "vindice.otlp.traces",
	domain.SignalMetrics: "vindice.otlp.metrics",
}

func Topic(sig domain.Signal) (string, bool) {
	t, ok := topics[sig]
	return t, ok
}

func SignalOf(topic string) (domain.Signal, bool) {
	for s, t := range topics {
		if t == topic {
			return s, true
		}
	}
	return "", false
}

func Topics() []string {
	return []string{topics[domain.SignalLogs], topics[domain.SignalTraces], topics[domain.SignalMetrics]}
}

// EnsureTopics creates the topics if they do not exist.
func EnsureTopics(ctx context.Context, brokers []string, partitions int32, retention time.Duration) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return err
	}
	defer cl.Close()

	cfg := map[string]*string{
		"max.message.bytes": kadm.StringPtr(strconv.Itoa(MaxMessageBytes)),
		"retention.ms":      kadm.StringPtr(strconv.FormatInt(retention.Milliseconds(), 10)),
		"compression.type":  kadm.StringPtr("producer"),
	}
	resp, err := kadm.NewClient(cl).CreateTopics(ctx, partitions, -1, cfg, Topics()...)
	if err != nil {
		return err
	}
	for _, r := range resp.Sorted() {
		if r.Err != nil && !errors.Is(r.Err, kerr.TopicAlreadyExists) {
			return r.Err
		}
	}
	return nil
}
