// Package kafkaconsumer drives the writer from Kafka: it decodes records,
// hands them to port.BatchWriter, flushes on size or time, and commits
// offsets only after a successful flush (at-least-once delivery).
package kafkaconsumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/b1ll1eee/vindice-obs/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/otlpmap"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

type Config struct {
	Brokers       []string
	Group         string
	BatchRows     int
	FlushInterval time.Duration
}

type Consumer struct {
	cl     *kgo.Client
	writer port.BatchWriter
	cfg    Config
	log    *slog.Logger
}

func New(cfg Config, writer port.BatchWriter, log *slog.Logger) (*Consumer, error) {
	// Rebalances are blocked while polling until AllowRebalance, so a batch
	// is always flushed and committed before its partitions can move.
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.Group),
		kgo.ConsumeTopics(kafka.Topics()...),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.FetchMaxBytes(64<<20),
		kgo.FetchMaxPartitionBytes(kafka.MaxMessageBytes),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return nil, err
	}
	return &Consumer{cl: cl, writer: writer, cfg: cfg, log: log}, nil
}

// Run consumes until ctx is cancelled, then flushes everything and commits.
func (c *Consumer) Run(ctx context.Context) error {
	defer c.cl.Close()
	lastFlush := time.Now()
	for {
		wait := max(c.cfg.FlushInterval-time.Since(lastFlush), 10*time.Millisecond)
		pollCtx, cancel := context.WithTimeout(ctx, wait)
		fetches := c.cl.PollRecords(pollCtx, c.cfg.BatchRows)
		cancel()
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachError(func(topic string, p int32, err error) {
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				c.log.Warn("kafka fetch error", "topic", topic, "partition", p, "err", err)
			}
		})
		fetches.EachRecord(c.handle)

		if ctx.Err() != nil {
			// Shutdown: decide pending traces, write everything, commit.
			finalCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return c.flushAndCommit(finalCtx, true)
		}
		if c.writer.Buffered() >= c.cfg.BatchRows || time.Since(lastFlush) >= c.cfg.FlushInterval {
			if err := c.flushAndCommit(ctx, false); err != nil && ctx.Err() == nil {
				return err
			}
			lastFlush = time.Now()
		}
	}
}

func (c *Consumer) handle(r *kgo.Record) {
	sig, ok := kafka.SignalOf(r.Topic)
	if !ok {
		return
	}
	b, skipped, err := otlpmap.Decode(sig, r.Value, r.Timestamp)
	if err != nil {
		// A record that cannot be decoded never will be; skip it.
		c.log.Warn("dropping undecodable record", "topic", r.Topic, "partition", r.Partition, "offset", r.Offset, "err", err)
		return
	}
	if skipped > 0 {
		c.log.Debug("skipped unsupported metric points", "count", skipped)
	}
	c.writer.Add(b, time.Now())
}

// flushAndCommit retries the flush with backoff until it succeeds or ctx ends.
// While retrying nothing is polled, so Kafka absorbs the backlog.
func (c *Consumer) flushAndCommit(ctx context.Context, final bool) error {
	backoff := time.Second
	for {
		err := c.writer.Flush(ctx, time.Now(), final)
		if err == nil {
			break
		}
		c.log.Error("flush failed, retrying", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	if err := c.cl.CommitUncommittedOffsets(ctx); err != nil {
		// Rows are already written; a lost commit only means re-reading them.
		c.log.Warn("offset commit failed", "err", err)
	}
	c.cl.AllowRebalance()
	return nil
}
