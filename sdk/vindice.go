// Package vindice connects a Go service to Vindice in one call: traces,
// metrics and logs over OTLP/HTTP, authenticated with an ingest API key.
//
//	sdk, err := vindice.Start(ctx, vindice.Config{ServiceName: "orders"})
//	if err != nil { ... }
//	defer sdk.Shutdown(context.Background())
//
//	log := sdk.Logger()                                   // slog, linked to traces
//	http.ListenAndServe(":8080", vindice.Handler(mux, "orders"))
//
// Endpoint and key default to VINDICE_ENDPOINT and VINDICE_API_KEY, so the
// key never has to be in the code.
package vindice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config describes the service. Only ServiceName is required.
type Config struct {
	ServiceName    string
	ServiceVersion string
	// Environment, e.g. "production"; sent as deployment.environment.name.
	Environment string
	// Attributes are added to every span, metric and log (resource attributes).
	Attributes map[string]string

	// Endpoint is the Vindice OTLP/HTTP address, e.g. "http://vindice:4318"
	// ("https://" for TLS). Default: $VINDICE_ENDPOINT, else http://localhost:4318.
	Endpoint string
	// APIKey is the ingest key (vdc_...). Default: $VINDICE_API_KEY.
	APIKey string

	// TraceSampleRatio keeps this share of traces at the source (0 means 1:
	// keep all). Vindice already samples on the server, keeping every
	// failing or slow trace, so lowering this is rarely needed.
	TraceSampleRatio float64
	// MetricInterval is how often metrics are sent. Default 15s.
	MetricInterval time.Duration
	// LogOutput also receives every log line as text (default os.Stderr);
	// set io.Discard to only send logs to Vindice.
	LogOutput io.Writer
}

// SDK holds the providers started by Start.
type SDK struct {
	tracer *sdktrace.TracerProvider
	meter  *sdkmetric.MeterProvider
	logs   *sdklog.LoggerProvider
	logger *slog.Logger
}

// Start configures OpenTelemetry for traces, metrics and logs, registers
// the providers globally (so instrumentation libraries pick them up) and
// returns the SDK. Call Shutdown before exit to flush what is buffered.
func Start(ctx context.Context, cfg Config) (*SDK, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("vindice: ServiceName is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = envOr("VINDICE_ENDPOINT", "http://localhost:4318")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("VINDICE_API_KEY")
	}
	if cfg.MetricInterval == 0 {
		cfg.MetricInterval = 15 * time.Second
	}
	if cfg.LogOutput == nil {
		cfg.LogOutput = os.Stderr
	}
	host, insecure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host), otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	}
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host), otlpmetrichttp.WithHeaders(headers),
		otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression),
	}
	logOpts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(host), otlploghttp.WithHeaders(headers),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		logOpts = append(logOpts, otlploghttp.WithInsecure())
	}

	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("vindice: trace exporter: %w", err)
	}
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("vindice: metric exporter: %w", err)
	}
	logExp, err := otlploghttp.New(ctx, logOpts...)
	if err != nil {
		return nil, fmt.Errorf("vindice: log exporter: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.AlwaysSample())
	if r := cfg.TraceSampleRatio; r > 0 && r < 1 {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(r))
	}
	s := &SDK{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithResource(res), sdktrace.WithSampler(sampler), sdktrace.WithBatcher(traceExp)),
		meter: sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(cfg.MetricInterval)))),
		logs: sdklog.NewLoggerProvider(
			sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp))),
	}

	otel.SetTracerProvider(s.tracer)
	otel.SetMeterProvider(s.meter)
	global.SetLoggerProvider(s.logs)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	s.logger = slog.New(tee{
		slog.NewTextHandler(cfg.LogOutput, nil),
		otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(s.logs)),
	})
	return s, nil
}

// Logger returns a slog.Logger that writes to LogOutput and sends to
// Vindice. Use the *Context methods (InfoContext, ErrorContext) inside a
// request so the log line is linked to its trace.
func (s *SDK) Logger() *slog.Logger { return s.logger }

// Shutdown flushes and stops all providers.
func (s *SDK) Shutdown(ctx context.Context) error {
	return errors.Join(s.tracer.Shutdown(ctx), s.meter.Shutdown(ctx), s.logs.Shutdown(ctx))
}

// Handler traces incoming HTTP requests and records request metrics.
func Handler(h http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(h, operation)
}

// Transport traces outgoing HTTP requests and passes the trace on, so calls
// between services join one trace.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{attribute.String("service.name", cfg.ServiceName)}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", cfg.Environment))
	}
	for k, v := range cfg.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	r, err := resource.New(ctx,
		resource.WithFromEnv(), // OTEL_RESOURCE_ATTRIBUTES
		resource.WithHost(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...), // last, so Config wins over env
	)
	if err != nil && r == nil {
		return nil, fmt.Errorf("vindice: resource: %w", err)
	}
	return r, nil // partial resources (e.g. no hostname) are fine
}

// parseEndpoint accepts "http://host:port", "https://host:port" or
// "host:port" (plain HTTP).
func parseEndpoint(e string) (host string, insecure bool, err error) {
	if !strings.Contains(e, "://") {
		return e, true, nil
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false, fmt.Errorf("vindice: invalid endpoint %q", e)
	}
	return u.Host, u.Scheme == "http", nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// tee sends each record to every handler.
type tee []slog.Handler

func (t tee) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t tee) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (t tee) WithAttrs(a []slog.Attr) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(a)
	}
	return out
}

func (t tee) WithGroup(name string) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}
