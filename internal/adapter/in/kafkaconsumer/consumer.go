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

	"github.com/b1ll1eee/vindice-observability/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-observability/internal/adapter/otlpmap"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
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

	// rowsPerRecord is a running estimate of rows per Kafka record. One
	// record carries a whole OTLP request, so BatchRows (rows) cannot be used
	// as a record count; polls are sized from this estimate instead.
	rowsPerRecord float64
}

// fetchMaxBytes bounds what the Kafka client buffers per fetch. Kafka still
// returns a record larger than this on its own, so big requests get through.
const fetchMaxBytes = 16 << 20

// pollLimit is how many records to take so the buffer reaches about
// BatchRows rows. Until there is an estimate it takes one record to learn.
func (c *Consumer) pollLimit() int {
	if c.rowsPerRecord <= 0 {
		return 1
	}
	room := c.cfg.BatchRows - c.writer.Buffered()
	if room <= 0 {
		// Rows held back (traces awaiting a sampling decision) can fill the
		// buffer on their own. A flush follows every such poll, so take one
		// batch's worth rather than stalling at a record at a time.
		room = c.cfg.BatchRows
	}
	return max(1, min(c.cfg.BatchRows, int(float64(room)/c.rowsPerRecord)))
}

// observe updates the rows-per-record estimate from one poll.
func (c *Consumer) observe(records, rows int) {
	if records == 0 {
		return
	}
	got := float64(max(rows, 0)) / float64(records)
	if c.rowsPerRecord <= 0 {
		c.rowsPerRecord = max(got, 1)
		return
	}
	c.rowsPerRecord = max(0.7*c.rowsPerRecord+0.3*got, 1)
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
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxPartitionBytes(fetchMaxBytes),
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
		fetches := c.cl.PollRecords(pollCtx, c.pollLimit())
		cancel()
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachError(func(topic string, p int32, err error) {
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				c.log.Warn("kafka fetch error", "topic", topic, "partition", p, "err", err)
			}
		})
		before := c.writer.Buffered()
		fetches.EachRecord(c.handle)
		// Rows can also leave the buffer (dropped by rules), so this is an
		// estimate of rows added, which is what sizing the next poll needs.
		c.observe(fetches.NumRecords(), c.writer.Buffered()-before)

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
