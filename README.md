# Vindice

Lightweight, self-hosted observability for logs, traces and metrics. Data comes in over OpenTelemetry (OTLP), is buffered in Kafka, and is stored in ClickHouse.

> **Status:** phases 1–4 are done: ingest, storage, query API, UI (services, logs, traces, metrics explorer, dashboards), alerting to Telegram, sign-in with roles, API keys for ingest, and Prometheus remote write.

```
app (OTel SDK) ──OTLP──▶ ingest ──zstd──▶ Kafka ──▶ writer ──batch──▶ ClickHouse
                gRPC :4317                         drop rules · tail sampling      │
                HTTP :4318                         RED stats · dedupe · rollups    │
                                                                                   ▼
                                         browser ◀── UI + REST API :8080 ◀── query
```

## Quick start

```bash
make up                  # Kafka + ClickHouse + vindice (docker compose)
open http://localhost:8080   # sign in, then create a key under API keys
make demo KEY=vdc_...    # send sample traces, logs and metrics
make stats               # rows and on-disk size per table
```

Point any OpenTelemetry SDK or Collector at `localhost:4317` (gRPC) or `http://localhost:4318` (HTTP), with an API key:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://vindice-host:4318
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer vdc_..."
```

## UI

- **Services.** Request rate, error rate and p50/p95/p99 per service, plus log and error-log counts. Click a service to see the same figures per operation.
- **Logs.** Full-text search, filters for service and severity, a histogram coloured by level, and paging. Each log links to its trace.
- **Traces.** Filters for service, minimum duration and errors only.
- **Trace view.** A waterfall with span details, events and attributes. The logs emitted inside the trace appear on the same page, and can be narrowed to the selected span.
- **Explore.** Chart any metric. Choose an aggregation (`avg`, `sum`, `min`, `max`, `count`, `last`, or `rate`, which handles counter resets), filter by attributes and group by one attribute. Add the chart to a dashboard.
- **Dashboards.** A grid of panels. Each panel charts a service RED measure, a metric query or a log count. Raw samples, 1-minute or 1-hour rollups are picked automatically from the time range.
- **Alerts.** A rule watches the same kinds of signals as a panel. It fires when the value over a window crosses a threshold for a set time, and moves through `ok → pending → firing → ok`. Notifications go to Telegram on firing and on resolve. A **Test** button sends a sample message.

## Sign-in and roles

Every page and `/api/v1` route requires signing in.

| Role | Can |
|---|---|
| `viewer` | read everything |
| `editor` | + create and change dashboards and alerts |
| `admin` | + manage users (**Users** page) |

On the very first start, an admin user is created:
- **Username** comes from `VINDICE_ADMIN_USERNAME` (default `admin`).
- **Password** comes from `VINDICE_ADMIN_PASSWORD`. If that is empty, a random password is generated and printed once in the log. Find it with `make logs | grep "first admin"`, then change it on the **Account** page.

Security details:
- **Passwords** are hashed with bcrypt.
- **Sessions** are signed cookies: HMAC-SHA256, HttpOnly, SameSite=Lax, and Secure when `VINDICE_PUBLIC_URL` uses https. They last `VINDICE_SESSION_TTL` (default 7 days).
- **Ending sessions everywhere.** Each user has a session version. Changing the password, signing out or disabling the user bumps it, which ends that user's sessions on every device.
- **Lockout.** Five failed sign-ins within 15 minutes, from the same address for the same username, lock that pair for 15 minutes.
- **CSRF.** State-changing requests from another origin are refused.
- **Safe user management.** The last active admin cannot be demoted, disabled or deleted, and admins cannot delete themselves.
- **Session key.** If `VINDICE_SESSION_SECRET` is empty, a key is generated and stored in ClickHouse, shared by every instance.

## Prometheus remote write

Vindice accepts Prometheus remote write 1.0 at `POST /api/v1/write` on port 4318, so the scrapes and exporters you already have can feed Vindice without changes. See `examples/prometheus-remote-write.yml`:

```yaml
remote_write:
  - url: http://vindice-host:4318/api/v1/write
    authorization:
      credentials: vdc_...
```

How samples are mapped:
- **Into the normal metric path.** Samples become OTLP metrics, then go through the same Kafka, writer and rollups.
- **Labels.** `job` becomes `service.name`, `instance` becomes `service.instance.id`, and every other label becomes an attribute.
- **Counters and gauges.** Prometheus metadata decides when it is sent. Otherwise, names ending in `_total`, `_bucket`, `_count` or `_sum` are stored as counters and everything else as gauges.
- **Dropped samples.** Staleness markers and native histograms are dropped.
- **Errors.** A 5xx response makes Prometheus retry. A bad key gets 401.
- **Remote write 2.0** gets 415. Keep the default `protobuf_message: prometheus.WriteRequest`.

## Ingest API keys

The OTLP ports accept data only with an API key. Admins create keys on the **API keys** page.

**Sending a key**
- Send it as `Authorization: Bearer <key>`, or as `X-Vindice-Key: <key>` for clients that cannot set `Authorization`.
- gRPC reads the same names from metadata.
- A missing or wrong key gets HTTP 401, or gRPC `Unauthenticated`. OTLP clients do not retry these.

**Storing keys**
- Keys start with `vdc_` and carry 256 random bits.
- Only their SHA-256 is stored. The key itself is shown once, when it is created.
- The UI shows when each key was last used, to the minute.

**How ingest checks keys**
- Ingest keeps the active keys in memory and reloads them every 30 seconds, so requests never wait on ClickHouse.
- If ClickHouse goes down, ingest keeps using the last loaded keys.
- A revoked key stops working within 30 seconds.
- Before the first load, ingest answers 503, so clients retry instead of dropping data.

Set `VINDICE_INGEST_AUTH=off` to accept data without a key, for local development only.

## Alerts to Telegram

1. Create a bot with [@BotFather](https://t.me/BotFather) and add it to your group.
2. Find the chat id with `curl https://api.telegram.org/bot<TOKEN>/getUpdates`. Group ids are negative.
3. Copy `deploy/.env.example` to `deploy/.env` and set `VINDICE_TELEGRAM_BOT_TOKEN` and `VINDICE_TELEGRAM_CHAT_ID`. The `.env` file is git-ignored.
4. Run `make up`, create a rule under **Alerts**, then press **Test**.

Without a token, alerts are still evaluated and recorded, and the messages go to the vindice log instead (see `make logs`).

How the evaluator behaves:
- **One evaluator only.** Run exactly one process with `-mode=all` or `-mode=alert`, otherwise every notification is sent twice.
- **State survives restarts.** Rule state is stored in ClickHouse, so a restart neither re-sends nor forgets a firing alert.
- **Failed queries are not recoveries.** A failed query keeps the current state.
- **Telegram errors are handled.** The notifier retries rate limits (`retry_after`) and 5xx responses, and removes the bot token from every error message.

The UI is Vue 3 + TypeScript with no UI or chart library. It is built into `web/dist` and embedded into the Go binary, so a deployment is still one binary. For development, run `make web-dev` (hot reload on :5173, which proxies `/api` to :8080).

### API

| Endpoint | |
|---|---|
| `GET /api/v1/services` | RED and log counts per service |
| `GET /api/v1/services/{service}/operations` | RED per operation |
| `GET /api/v1/logs` | search: `service`, `severity`, `q`, `trace_id`, `before` (cursor), `limit` |
| `GET /api/v1/logs/histogram` | log counts per level, with the same filters |
| `GET /api/v1/traces` | root spans: `service`, `min_duration_ms`, `errors`, `limit` |
| `GET /api/v1/traces/{id}` | spans, logs and resources of one trace |
| `GET /api/v1/services/{service}/series` | RED over time |
| `GET /api/v1/metrics` | metric names |
| `GET /api/v1/metrics/query` | `name`, `agg`, `service`, `group_by`, `filter=key:value` (repeatable) |
| `GET POST /api/v1/dashboards`, `GET PUT DELETE /api/v1/dashboards/{id}` | dashboards |
| `GET POST /api/v1/alerts`, `GET PUT DELETE /api/v1/alerts/{id}` | alert rules, with current state |
| `POST /api/v1/alerts/{id}/test` | send a test notification |
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me` | session |
| `POST /api/v1/auth/password` | change your password |
| `GET POST /api/v1/users`, `PUT DELETE /api/v1/users/{id}` | users (admin) |
| `GET POST /api/v1/keys`, `DELETE /api/v1/keys/{id}` | ingest API keys (admin); the secret is returned only by POST |
| `GET /api/v1/alerts/events` | firing and resolved history |

`from` and `to` are epoch milliseconds. Without them, the last hour is used.

## Architecture

The code follows hexagonal architecture (ports and adapters). The core does not import any infrastructure package.

```
cmd/vindice/                composition root: builds adapters, injects them into the core
internal/core/
  domain/                   entities and query objects: LogRow, SpanRow, Series, LogQuery, TraceQuery, ...
  port/                     inbound (Ingestor, BatchWriter, Querier, MetricsQuerier, DashboardManager, AlertManager,
                            Authenticator, UserManager, IngestAuthorizer, KeyManager)
                            outbound (Publisher, *Repository, TelemetryReader, Notifier, UserRepository,
                            SecretStore, PasswordHasher, SessionCodec, APIKeyRepository)
  service/                  IngestService, WriterService, QueryService, DashboardService, AlertService, AuthService, APIKeyService
  policy/                   drop rules, sampling, attribute promotion, dedupe
internal/adapter/
  in/otlphttp, in/otlpgrpc  OTLP receivers → port.Ingestor
  in/promwrite              Prometheus remote write 1.0 → OTLP → port.Ingestor
  in/ingestauth             API key check shared by the HTTP receivers
  in/kafkaconsumer          Kafka → port.BatchWriter (commits offsets after each flush)
  in/httpapi                REST API → port.Querier, serves the UI
  out/kafkapublisher        port.Publisher
  out/clickhouse            write repositories, TelemetryReader, dashboard/alert stores, schema migration
  out/telegram              port.Notifier on the Telegram Bot API (LogNotifier when no token)
  out/authcrypto            bcrypt PasswordHasher, HMAC SessionCodec
  otlpmap/                  OTLP protobuf → domain
web/                        Vue UI, embedded into the binary
```

Reads and writes go through separate ports (CQRS). This lets the read side use rollups and the query cache without touching the write path.

| Pattern | Where |
|---|---|
| Ports & Adapters | `core/port` interfaces, `adapter/*` implementations |
| Repository | `out/clickhouse` implements the `port.*Repository` interfaces, one per aggregate |
| Strategy + Factory | `policy.Sampler`: `NewSampler` picks keep-all or tail sampling |
| Specification | `policy.Filter`: each drop rule combines small `Spec`s with `allOf`/`anyOf` |
| Mapper | `otlpmap` translates OTLP into domain entities, and `httpapi` turns domain entities into DTOs |
| Query Object | `domain.LogQuery` and `domain.TraceQuery` carry search criteria |
| Builder | `clickhouse.where` assembles WHERE clauses with bound arguments |
| State machine | `service.transition` moves an alert through ok → pending → firing → ok |
| Null Object | `telegram.LogNotifier` is used when no bot token is set |
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
| `VINDICE_INGEST_AUTH` | `required` | `off` accepts OTLP without an API key |
| `VINDICE_RETENTION_*_DAYS` | 3/14/90/7/7/30/365 | debug/info/error logs, traces, metrics raw/1m/1h |
| `VINDICE_CH_COLD_STORAGE_POLICY` | – | enables the S3/R2 cold tier |

Run modes: `vindice -mode=all|ingest|writer|query|alert`. Each part scales independently, except `alert`, which must run as a single instance. The query API listens on `VINDICE_QUERY_ADDR` (default `:8080`). Alert settings are `VINDICE_TELEGRAM_BOT_TOKEN`, `VINDICE_TELEGRAM_CHAT_ID`, `VINDICE_ALERT_INTERVAL` (default `30s`) and `VINDICE_PUBLIC_URL`.

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
- **Lockout is per instance.** Failed sign-ins are counted in memory, so they reset on restart and are not shared between instances.
