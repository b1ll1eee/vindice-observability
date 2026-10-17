// Command loadgen sends a deterministic set of OTLP log records over HTTP/protobuf.
// The same seed always produces the same records, so two backends receive identical data.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logs "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

var services = []string{"checkout", "payments", "orders", "users", "search", "inventory", "notifications", "gateway"}

type level struct {
	num  logs.SeverityNumber
	text string
}

var (
	debug = level{logs.SeverityNumber_SEVERITY_NUMBER_DEBUG, "DEBUG"}
	info  = level{logs.SeverityNumber_SEVERITY_NUMBER_INFO, "INFO"}
	warn  = level{logs.SeverityNumber_SEVERITY_NUMBER_WARN, "WARN"}
	errl  = level{logs.SeverityNumber_SEVERITY_NUMBER_ERROR, "ERROR"}
)

// Needle is a token written into exactly NeedleCount records, spread across the data.
const (
	Needle      = "needle7f3a9c"
	NeedleCount = 50
)

func str(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}

func record(r *rand.Rand, i int, total int, t0 time.Time, span time.Duration) (string, *logs.LogRecord) {
	svc := services[r.IntN(len(services))]
	p := r.IntN(100)
	lv := debug
	switch {
	case p >= 95:
		lv = errl
	case p >= 85:
		lv = warn
	case p >= 40:
		lv = info
	}
	user := fmt.Sprintf("u-%06d", r.IntN(200000))
	order := fmt.Sprintf("ord-%08d", i)
	var body string
	switch lv {
	case errl:
		reasons := []string{"upstream timeout after 3000ms", "connection reset by peer", "insufficient funds", "deadlock detected, retrying"}
		body = fmt.Sprintf("request failed for order %s user %s: %s", order, user, reasons[r.IntN(len(reasons))])
	case warn:
		body = fmt.Sprintf("slow query took %dms for user %s on order %s", 200+r.IntN(4000), user, order)
	case info:
		body = fmt.Sprintf("order %s paid by user %s amount %d.%02d THB via promptpay", order, user, r.IntN(5000), r.IntN(100))
	default:
		body = fmt.Sprintf("cache lookup key=session:%s hit=%t latency_us=%d", user, r.IntN(2) == 0, r.IntN(900))
	}
	if i%(total/NeedleCount) == 0 {
		body += " marker " + Needle
	}
	ts := t0.Add(time.Duration(int64(span) * int64(i) / int64(total)))
	return svc, &logs.LogRecord{
		TimeUnixNano:   uint64(ts.UnixNano()),
		SeverityNumber: lv.num,
		SeverityText:   lv.text,
		Body:           &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: body}},
		Attributes:     []*common.KeyValue{str("user_id", user), str("order_id", order)},
	}
}

func batch(recs map[string][]*logs.LogRecord) ([]byte, error) {
	req := &collogs.ExportLogsServiceRequest{}
	for svc, rs := range recs {
		req.ResourceLogs = append(req.ResourceLogs, &logs.ResourceLogs{
			Resource:  &resource.Resource{Attributes: []*common.KeyValue{str("service.name", svc)}},
			ScopeLogs: []*logs.ScopeLogs{{LogRecords: rs}},
		})
	}
	return proto.Marshal(req)
}

func main() {
	url := flag.String("url", "", "OTLP/HTTP logs endpoint, e.g. http://localhost:4318/v1/logs")
	header := flag.String("auth", "", "Authorization header value (optional)")
	total := flag.Int("n", 2_000_000, "number of log records")
	size := flag.Int("batch", 1000, "records per request")
	workers := flag.Int("c", 8, "concurrent requests")
	span := flag.Duration("span", 50*time.Minute, "time range the records are spread over, ending now")
	start := flag.Int64("t0", 0, "start timestamp in unix ms (0: now - span)")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "-url is required")
		os.Exit(2)
	}
	t0 := time.Now().Add(-*span)
	if *start > 0 {
		t0 = time.UnixMilli(*start)
	}

	// Build every payload first so encoding cost is not part of the timing.
	r := rand.New(rand.NewPCG(42, 7))
	var payloads [][]byte
	var raw int64
	for i := 0; i < *total; i += *size {
		recs := map[string][]*logs.LogRecord{}
		for j := i; j < min(i+*size, *total); j++ {
			svc, rec := record(r, j, *total, t0, *span)
			raw += int64(len(rec.Body.GetStringValue()))
			recs[svc] = append(recs[svc], rec)
		}
		b, err := batch(recs)
		if err != nil {
			panic(err)
		}
		payloads = append(payloads, b)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	jobs := make(chan []byte)
	var sent, failed atomic.Int64
	var wg sync.WaitGroup
	began := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				for attempt := 0; ; attempt++ {
					req, _ := http.NewRequest(http.MethodPost, *url, bytes.NewReader(p))
					req.Header.Set("Content-Type", "application/x-protobuf")
					if *header != "" {
						req.Header.Set("Authorization", *header)
					}
					resp, err := client.Do(req)
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if resp.StatusCode/100 == 2 {
							sent.Add(1)
							break
						}
						err = fmt.Errorf("status %d", resp.StatusCode)
					}
					if attempt >= 20 {
						failed.Add(1)
						fmt.Fprintln(os.Stderr, "giving up on a batch:", err)
						break
					}
					time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				}
			}
		}()
	}
	for _, p := range payloads {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	el := time.Since(began)
	fmt.Printf(`{"records":%d,"batches_ok":%d,"batches_failed":%d,"seconds":%.2f,"records_per_sec":%.0f,"raw_body_bytes":%d,"t0_ms":%d,"t1_ms":%d}`+"\n",
		*total, sent.Load(), failed.Load(), el.Seconds(), float64(*total)/el.Seconds(), raw, t0.UnixMilli(), t0.Add(*span).UnixMilli())
}
