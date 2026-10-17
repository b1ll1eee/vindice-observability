package kafkaconsumer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-observability/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

// countingWriter remembers how many rows were buffered at each flush.
type countingWriter struct {
	mu       sync.Mutex
	buffered int
	written  int
	maxFlush int
}

func (w *countingWriter) Add(b domain.Batch, _ time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffered += len(b.Logs)
}

func (w *countingWriter) Buffered() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffered
}

func (w *countingWriter) Flush(context.Context, time.Time, bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxFlush = max(w.maxFlush, w.buffered)
	w.written += w.buffered
	w.buffered = 0
	return nil
}

func (w *countingWriter) snapshot() (written, maxFlush int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written, w.maxFlush
}

func logRequest(rows int) []byte {
	recs := make([]*logspb.LogRecord, rows)
	for i := range recs {
		recs[i] = &logspb.LogRecord{
			TimeUnixNano: uint64(time.Now().UnixNano()),
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("line %d", i)}},
		}
	}
	b, err := proto.Marshal(&collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name",
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "svc"}}}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: recs}},
	}}})
	if err != nil {
		panic(err)
	}
	return b
}

// A backlog must be written in batches of about BatchRows rows, not pulled
// into memory in one go. BatchRows counts rows, while one Kafka record holds a
// whole OTLP request, so the consumer has to stop on rows, not on records.
func TestBacklogIsFlushedInRowSizedBatches(t *testing.T) {
	const (
		records       = 200
		rowsPerRecord = 1000
		batchRows     = 10_000
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	brokers := cluster.ListenAddrs()
	if err := kafka.EnsureTopics(ctx, brokers, 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	topic, _ := kafka.Topic(domain.SignalLogs)

	// Build the backlog before the consumer starts, as after an outage.
	pr, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ProducerBatchMaxBytes(kafka.MaxMessageBytes))
	if err != nil {
		t.Fatal(err)
	}
	payload := logRequest(rowsPerRecord)
	for range records {
		pr.Produce(ctx, &kgo.Record{Topic: topic, Value: payload}, nil)
	}
	if err := pr.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	pr.Close()

	w := &countingWriter{}
	c, err := New(Config{Brokers: brokers, Group: "t", BatchRows: batchRows, FlushInterval: time.Hour},
		w, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- c.Run(runCtx) }()

	for {
		if written, _ := w.snapshot(); written >= records*rowsPerRecord-batchRows {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("timed out waiting for the backlog to be written")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	written, maxFlush := w.snapshot()
	if written != records*rowsPerRecord {
		t.Errorf("wrote %d rows, want %d", written, records*rowsPerRecord)
	}
	if limit := batchRows + rowsPerRecord; maxFlush > limit {
		t.Errorf("a flush held %d rows; want at most %d (BatchRows plus one record)", maxFlush, limit)
	}
}

func TestPollLimit(t *testing.T) {
	w := &countingWriter{}
	c := &Consumer{writer: w, cfg: Config{BatchRows: 10_000}}
	if got := c.pollLimit(); got != 1 {
		t.Errorf("first poll takes %d records; want 1 to learn the record size", got)
	}
	c.observe(1, 1000)
	if got := c.pollLimit(); got != 10 {
		t.Errorf("with 1000 rows per record: %d records; want 10", got)
	}
	w.buffered = 7_000
	if got := c.pollLimit(); got != 3 {
		t.Errorf("with 7000 rows buffered: %d records; want 3", got)
	}
	w.buffered = 50_000 // held-back traces alone exceed BatchRows
	if got := c.pollLimit(); got != 10 {
		t.Errorf("with the buffer already full: %d records; want a full batch (10)", got)
	}
	c2 := &Consumer{writer: &countingWriter{}, cfg: Config{BatchRows: 10_000}}
	c2.observe(5, 0) // every row dropped by rules
	if got := c2.pollLimit(); got != 10_000 {
		t.Errorf("with tiny records: %d; want BatchRows as the cap", got)
	}
}
