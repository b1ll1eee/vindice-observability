# Vindice Go SDK

Connect a Go service to [Vindice](../README.md) in one call. It sends traces, metrics and logs over OTLP/HTTP and authenticates with an ingest API key.

```bash
go get github.com/b1ll1eee/vindice-observability/sdk
```

```go
import vindice "github.com/b1ll1eee/vindice-observability/sdk"

func main() {
	sdk, err := vindice.Start(context.Background(), vindice.Config{
		ServiceName:    "orders",
		ServiceVersion: "1.4.0",
		Environment:    "production",
	})
	if err != nil {
		panic(err)
	}
	defer sdk.Shutdown(context.Background()) // flush before exit

	log := sdk.Logger() // *slog.Logger: prints locally and sends to Vindice

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		log.InfoContext(r.Context(), "loading order", "id", r.PathValue("id")) // linked to the trace
	})
	http.ListenAndServe(":8080", vindice.Handler(mux, "orders"))
}
```

Run it with the endpoint and key in the environment, so the key never sits in the code:

```bash
VINDICE_ENDPOINT=http://vindice-host:4318 VINDICE_API_KEY=vdc_... ./orders
```

## What you get

- **Traces.**
  - `vindice.Handler` traces incoming requests.
  - `vindice.Transport` traces outgoing calls and passes the trace on, so calls between services join one trace.
  - Any library that uses the global OpenTelemetry provider is picked up too, for example database drivers with otel support.
- **Logs.** `sdk.Logger()` is a `slog.Logger`. Logs written with the `*Context` methods carry the trace and span id, so the Vindice trace view shows them next to the spans.
- **Metrics.** `otel.Meter("...")` works as usual, and request metrics come from `Handler`. Metrics are sent every 15 seconds.
- **Resource.** Each span, metric and log carries the service name, version, environment, host and Go version, plus `Config.Attributes` and `OTEL_RESOURCE_ATTRIBUTES`.

## Config

| Field | Default | |
|---|---|---|
| `ServiceName` | required | |
| `Endpoint` | `$VINDICE_ENDPOINT`, else `http://localhost:4318` | `https://` turns on TLS |
| `APIKey` | `$VINDICE_API_KEY` | created on the Vindice **API keys** page |
| `TraceSampleRatio` | 1 (keep all) | Vindice already samples on the server and keeps every failing or slow trace |
| `MetricInterval` | 15s | |
| `LogOutput` | `os.Stderr` | `io.Discard` sends logs only to Vindice |

Requires Go 1.26 or newer. A runnable service is in [`example/`](example/main.go).

MIT licensed, see [LICENSE](LICENSE).
