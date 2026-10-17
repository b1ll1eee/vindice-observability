# Vindice

Lightweight, self-hosted observability for logs, traces and metrics. Data comes in over OpenTelemetry (OTLP), is buffered in Kafka, and is stored in ClickHouse.

> **Status:** phase 1 is done: ingest, buffering and storage. The query API, UI, dashboards and alerting are next.

```
app (OTel SDK) ──OTLP──▶ ingest ──zstd──▶ Kafka ──▶ writer ──batch──▶ ClickHouse
                gRPC :4317                         drop rules · tail sampling
                HTTP :4318                         RED stats · dedupe · rollups
```

## Quick start

```bash
make up      # Kafka + ClickHouse + vindice (docker compose)
make demo    # send sample traces, logs and metrics
make stats   # rows and on-disk size per table
```

Point any OpenTelemetry SDK or Collector at `localhost:4317` (gRPC) or `http://localhost:4318` (HTTP).

## Architecture

The code follows hexagonal architecture (ports and adapters). The core does not import any infrastructure package.

```
cmd/vindice/                composition root: builds adapters, injects them into the core
internal/core/
  domain/                   entities: LogRow, SpanRow, Series, Sample, Resource, Batch
  port/                     inbound (Ingestor, BatchWriter) and outbound (Publisher, *Repository) interfaces
  service/                  IngestService, WriterService
  policy/                   drop rules, sampling, attribute promotion, dedupe
internal/adapter/
  in/otlphttp, in/otlpgrpc  OTLP receivers → port.Ingestor
  in/kafkaconsumer          Kafka → port.BatchWriter (commits offsets after each flush)
  out/kafkapublisher        port.Publisher
  out/clickhouse            repositories + schema migration
  otlpmap/                  OTLP protobuf → domain
```

| Pattern | Where |
|---|---|
| Ports & Adapters | `core/port` interfaces, `adapter/*` implementations |
| Repository | `out/clickhouse` implements the `port.*Repository` interfaces, one per aggregate |
| Strategy + Factory | `policy.Sampler`: `NewSampler` picks keep-all or tail sampling |
| Specification | `policy.Filter`: each drop rule combines small `Spec`s with `allOf`/`anyOf` |
| Mapper | `otlpmap` translates OTLP into domain entities |
| Dependency Injection | constructors only, wired in `cmd/vindice/main.go` |

## Optimizations

**Less data stored**
- **Tail sampling.** Traces with an error or a slow span are always kept. Of the remaining traces, `VINDICE_SAMPLE_RATE` are kept, chosen consistently by trace ID.
- **Exact RED metrics.** Entry spans feed `red_1m` before sampling, through a `Null` table, so sampling does not skew request rates, errors or latency.
- **Retention by log severity.** DEBUG logs are kept 3 days, INFO/WARN and logs without a severity 14 days, ERROR 90 days.
- **Drop rules.** Rules in `examples/rules.json` remove noise such as health checks before anything is stored.
- **Metric downsampling.** Raw samples are kept 7 days, 1-minute rollups 30 days, 1-hour rollups 1 year.

**Smaller storage**
- **Binary IDs.** Trace and span IDs are stored as `FixedString`, half the size of hex.
- **Codecs per column type:**
  - timestamps use `DoubleDelta`
  - metric values use `Gorilla`
  - log bodies and attribute maps use `ZSTD(3)`
  - repeated strings use `LowCardinality` or `Enum8`
- **Deduplicated dimensions.** Resource attributes and metric series are stored once and referenced by a hash ID.
- **Promoted HTTP attributes.** `http.method`, `http.route` and `http.status_code` are moved from the attribute map into their own columns.

**Faster reads**
- **Pre-aggregated tables.** `red_1m` and `log_counts_1m` are materialized views that dashboards and alerts read instead of raw rows.
- **Trace lookup.** `trace_index` maps a trace ID to its time window. Bloom filters cover trace IDs and a token index covers log bodies.

**Cheap to run**
- **Kafka buffer.** Ingest acknowledges a request only after Kafka has it. If ClickHouse is down, the writer retries and Kafka holds the backlog.
- **Large batches.** Inserts are sent in batches, which keeps ClickHouse merges to a minimum.
- **Low-memory ClickHouse config.** It uses small caches, `max_threads=2` and keeps only `query_log` among the system logs.
- **Optional cold tier.** Old parts can move to S3 or R2 via `deploy/clickhouse/optional/cold-storage.xml`.

## Configuration

All settings come from environment variables. Defaults are in `internal/config/config.go`.

| Variable | Default | |
|---|---|---|
| `VINDICE_KAFKA_BROKERS` | `localhost:19092` | |
| `VINDICE_CH_ADDR` | `localhost:9000` | |
| `VINDICE_SAMPLE_RATE` | `0.1` | fraction of normal traces kept; `1` keeps everything |
| `VINDICE_SAMPLE_SLOW` | `1s` | traces with a span at least this slow are always kept |
| `VINDICE_RULES_FILE` | – | JSON drop rules |
| `VINDICE_RETENTION_*_DAYS` | 3/14/90/7/7/30/365 | debug/info/error logs, traces, metrics raw/1m/1h |
| `VINDICE_CH_COLD_STORAGE_POLICY` | – | enables the S3/R2 cold tier |

Run modes: `vindice -mode=all|ingest|writer`. Ingest and writer scale independently.

## Tests

```bash
make test                                        # unit tests
VINDICE_IT_CH_ADDR=localhost:9000 \
  go test -tags integration ./internal/integration/   # end-to-end: real ClickHouse + in-process Kafka
```

## Known limits

- **Tail sampling needs one writer.** It holds traces in memory, so for now only one writer should run. Running several writers needs Kafka partitioning by trace ID.
- **Crash loss window.** Traces still waiting for a sampling decision (at most `VINDICE_SAMPLE_WAIT`) are lost if the writer crashes.
- **Exponential histograms are skipped.** Every other OTLP metric type is stored.
