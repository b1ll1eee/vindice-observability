// Package config loads settings from VINDICE_* environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	KafkaBrokers    []string
	KafkaGroup      string
	KafkaPartitions int32
	KafkaRetention  time.Duration

	ClickHouseAddrs    []string
	ClickHouseDB       string
	ClickHouseUser     string
	ClickHousePassword string
	// ColdStoragePolicy enables moving old parts to a 'cold' volume (S3/R2).
	// The policy must exist in the ClickHouse server config.
	ColdStoragePolicy string
	ColdAfterDays     int

	OTLPGRPCAddr    string
	OTLPHTTPAddr    string
	MaxRequestBytes int64
	QueryAddr       string
	// IngestAuth is "required" (OTLP needs an API key) or "off".
	IngestAuth string
	// PublicURL is where users reach the UI; alert messages link to it.
	PublicURL string

	AdminUsername string
	// AdminPassword seeds the first admin; when empty a random one is logged once.
	AdminPassword string
	// SessionSecret signs session cookies; when empty a key is generated and
	// stored in ClickHouse, shared by all instances.
	SessionSecret string
	SessionTTL    time.Duration

	AlertInterval  time.Duration
	TelegramToken  string
	TelegramChatID string
	TelegramAPI    string

	BatchRows     int
	FlushInterval time.Duration
	RulesFile     string

	SampleRate       float64
	SampleSlow       time.Duration
	SampleWait       time.Duration
	SampleMaxPending int

	Retention Retention

	LogLevel slog.Level
}

// Retention is in days.
type Retention struct {
	DebugLogs  int // severity TRACE/DEBUG
	InfoLogs   int // INFO/WARN and logs without severity
	ErrorLogs  int // ERROR/FATAL
	Traces     int
	MetricsRaw int
	Metrics1m  int
	Metrics1h  int
}

func Load() (Config, error) {
	e := &env{}
	c := Config{
		KafkaBrokers:    e.list("VINDICE_KAFKA_BROKERS", "localhost:19092"),
		KafkaGroup:      e.str("VINDICE_KAFKA_GROUP", "vindice-writer"),
		KafkaPartitions: int32(e.int("VINDICE_KAFKA_PARTITIONS", 3)),
		KafkaRetention:  e.dur("VINDICE_KAFKA_RETENTION", 24*time.Hour),

		ClickHouseAddrs:    e.list("VINDICE_CH_ADDR", "localhost:9000"),
		ClickHouseDB:       e.str("VINDICE_CH_DATABASE", "vindice"),
		ClickHouseUser:     e.str("VINDICE_CH_USER", "default"),
		ClickHousePassword: e.str("VINDICE_CH_PASSWORD", ""),
		ColdStoragePolicy:  e.str("VINDICE_CH_COLD_STORAGE_POLICY", ""),
		ColdAfterDays:      e.int("VINDICE_CH_COLD_AFTER_DAYS", 7),

		OTLPGRPCAddr:    e.str("VINDICE_OTLP_GRPC_ADDR", ":4317"),
		OTLPHTTPAddr:    e.str("VINDICE_OTLP_HTTP_ADDR", ":4318"),
		MaxRequestBytes: int64(e.int("VINDICE_MAX_REQUEST_BYTES", 16<<20)),
		QueryAddr:       e.str("VINDICE_QUERY_ADDR", ":8080"),
		IngestAuth:      e.str("VINDICE_INGEST_AUTH", "required"),
		PublicURL:       e.str("VINDICE_PUBLIC_URL", "http://localhost:8080"),

		AdminUsername: e.str("VINDICE_ADMIN_USERNAME", "admin"),
		AdminPassword: e.str("VINDICE_ADMIN_PASSWORD", ""),
		SessionSecret: e.str("VINDICE_SESSION_SECRET", ""),
		SessionTTL:    e.dur("VINDICE_SESSION_TTL", 7*24*time.Hour),

		AlertInterval:  e.dur("VINDICE_ALERT_INTERVAL", 30*time.Second),
		TelegramToken:  e.str("VINDICE_TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID: e.str("VINDICE_TELEGRAM_CHAT_ID", ""),
		TelegramAPI:    e.str("VINDICE_TELEGRAM_API", "https://api.telegram.org"),

		BatchRows:     e.int("VINDICE_BATCH_ROWS", 50_000),
		FlushInterval: e.dur("VINDICE_FLUSH_INTERVAL", 3*time.Second),
		RulesFile:     e.str("VINDICE_RULES_FILE", ""),

		SampleRate:       e.float("VINDICE_SAMPLE_RATE", 0.1),
		SampleSlow:       e.dur("VINDICE_SAMPLE_SLOW", time.Second),
		SampleWait:       e.dur("VINDICE_SAMPLE_WAIT", 10*time.Second),
		SampleMaxPending: e.int("VINDICE_SAMPLE_MAX_PENDING", 200_000),

		Retention: Retention{
			DebugLogs:  e.int("VINDICE_RETENTION_DEBUG_DAYS", 3),
			InfoLogs:   e.int("VINDICE_RETENTION_INFO_DAYS", 14),
			ErrorLogs:  e.int("VINDICE_RETENTION_ERROR_DAYS", 90),
			Traces:     e.int("VINDICE_RETENTION_TRACE_DAYS", 7),
			MetricsRaw: e.int("VINDICE_RETENTION_METRICS_RAW_DAYS", 7),
			Metrics1m:  e.int("VINDICE_RETENTION_METRICS_1M_DAYS", 30),
			Metrics1h:  e.int("VINDICE_RETENTION_METRICS_1H_DAYS", 365),
		},
	}
	if err := c.LogLevel.UnmarshalText([]byte(e.str("VINDICE_LOG_LEVEL", "info"))); err != nil {
		e.errs = append(e.errs, fmt.Sprintf("VINDICE_LOG_LEVEL: %v", err))
	}
	if c.IngestAuth != "required" && c.IngestAuth != "off" {
		e.errs = append(e.errs, "VINDICE_INGEST_AUTH must be required or off")
	}
	if c.SampleRate < 0 || c.SampleRate > 1 {
		e.errs = append(e.errs, "VINDICE_SAMPLE_RATE must be between 0 and 1")
	}
	if len(e.errs) > 0 {
		return c, fmt.Errorf("config: %s", strings.Join(e.errs, "; "))
	}
	return c, nil
}

type env struct{ errs []string }

func (e *env) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func (e *env) list(key, def string) []string {
	var out []string
	for _, s := range strings.Split(e.str(key, def), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (e *env) int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return n
}

func (e *env) float(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		e.errs = append(e.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return f
}

func (e *env) dur(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return d
}
