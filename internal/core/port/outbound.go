package port

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

// Publisher durably enqueues raw OTLP payloads for the writer.
type Publisher interface {
	Publish(ctx context.Context, sig domain.Signal, payload []byte) error
}

// The repositories below are segregated by aggregate (Repository pattern).

type ResourceRepository interface {
	SaveResources(ctx context.Context, rows []domain.Resource) error
}

type LogRepository interface {
	SaveLogs(ctx context.Context, rows []domain.LogRow) error
}

type TraceRepository interface {
	SaveSpans(ctx context.Context, rows []domain.SpanRow) error
	SaveSpanStats(ctx context.Context, rows []domain.SpanStat) error
}

type MetricRepository interface {
	SaveSeries(ctx context.Context, rows []domain.Series) error
	SaveSamples(ctx context.Context, rows []domain.Sample) error
}

type TelemetryRepository interface {
	ResourceRepository
	LogRepository
	TraceRepository
	MetricRepository
}

// TelemetryReader is the read-side repository. It is kept apart from the
// write repositories so reads can be optimised (rollups, caching) on their own.
type TelemetryReader interface {
	ServiceRED(ctx context.Context, r domain.TimeRange) ([]domain.ServiceSummary, error)
	ServiceLogCounts(ctx context.Context, r domain.TimeRange) (map[string]domain.LogCount, error)
	OperationRED(ctx context.Context, service string, r domain.TimeRange) ([]domain.OperationSummary, error)
	SearchLogs(ctx context.Context, q domain.LogQuery) ([]domain.LogEntry, error)
	// LogHistogram buckets log counts by step. rollup selects the per-minute
	// table, which is only valid without content filters and with step >= 1m.
	LogHistogram(ctx context.Context, q domain.LogQuery, step time.Duration, rollup bool) ([]domain.HistogramBucket, error)
	SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error)
	// TraceWindow returns when the trace's spans started; ok is false if unknown.
	TraceWindow(ctx context.Context, id [16]byte) (r domain.TimeRange, ok bool, err error)
	TraceSpans(ctx context.Context, id [16]byte, r domain.TimeRange) ([]domain.SpanRow, error)
	TraceLogs(ctx context.Context, id [16]byte, r domain.TimeRange) ([]domain.LogEntry, error)
	Resources(ctx context.Context, ids []uint64) (map[uint64]map[string]string, error)

	MetricNames(ctx context.Context, service string, r domain.TimeRange) ([]domain.MetricName, error)
	// MetricSeries returns at most limit series matching name, service and filters.
	MetricSeries(ctx context.Context, name, service string, filters map[string]string, limit int) ([]domain.SeriesInfo, error)
	MetricBuckets(ctx context.Context, ids []uint64, r domain.TimeRange, step time.Duration, res domain.Resolution) ([]domain.MetricBucket, error)
	ServiceSeries(ctx context.Context, service string, r domain.TimeRange, step time.Duration) ([]domain.REDPoint, error)
}

type DashboardRepository interface {
	ListDashboards(ctx context.Context) ([]domain.Dashboard, error)
	GetDashboard(ctx context.Context, id string) (domain.Dashboard, bool, error)
	SaveDashboard(ctx context.Context, d domain.Dashboard) error
	DeleteDashboard(ctx context.Context, id string) error
}

type AlertRuleRepository interface {
	ListRules(ctx context.Context) ([]domain.AlertRule, error)
	GetRule(ctx context.Context, id string) (domain.AlertRule, bool, error)
	SaveRule(ctx context.Context, r domain.AlertRule) error
	DeleteRule(ctx context.Context, id string) error
}

// AlertStateRepository persists evaluator state, so a restart neither
// re-sends nor forgets a firing alert, and keeps the event history.
type AlertStateRepository interface {
	LoadStatuses(ctx context.Context) (map[string]domain.AlertStatus, error)
	SaveStatuses(ctx context.Context, s []domain.AlertStatus) error
	AppendEvents(ctx context.Context, e []domain.AlertEvent) error
	Events(ctx context.Context, ruleID string, limit int) ([]domain.AlertEvent, error)
}

type UserRepository interface {
	ListUsers(ctx context.Context) ([]domain.User, error)
	GetUser(ctx context.Context, id string) (domain.User, bool, error)
	SaveUser(ctx context.Context, u domain.User) error
	DeleteUser(ctx context.Context, id string) error
}

type APIKeyRepository interface {
	ListKeys(ctx context.Context) ([]domain.APIKey, error)
	GetKey(ctx context.Context, id string) (domain.APIKey, bool, error)
	SaveKey(ctx context.Context, k domain.APIKey) error
	// RecordKeyUse appends last-use times. It never rewrites the key
	// itself, so it cannot undo a concurrent revoke.
	RecordKeyUse(ctx context.Context, used map[string]time.Time) error
	KeyLastUse(ctx context.Context) (map[string]time.Time, error)
}

// SecretStore keeps a named secret shared by every instance (e.g. the
// session signing key), creating it with gen the first time.
type SecretStore interface {
	GetOrCreateSecret(ctx context.Context, name string, gen func() string) (string, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify reports whether password matches hash.
	Verify(hash, password string) bool
}

// SessionCodec turns sessions into tamper-proof tokens and back.
type SessionCodec interface {
	Encode(s domain.Session) string
	// Decode rejects tokens that are forged or expired.
	Decode(token string, now time.Time) (domain.Session, error)
}

// Notifier delivers alert notifications (Telegram, ...).
type Notifier interface {
	Notify(ctx context.Context, n domain.Notification) error
}

// SchemaMigrator creates or updates the storage schema.
type SchemaMigrator interface {
	Migrate(ctx context.Context) error
}
