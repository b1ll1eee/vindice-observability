// Package port defines the interfaces between the core and the adapters.
// Inbound ports are implemented by core services and called by driving
// adapters; outbound ports are implemented by driven adapters.
package port

import (
	"context"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Ingestor accepts raw OTLP protobuf export requests (from HTTP and gRPC).
type Ingestor interface {
	Ingest(ctx context.Context, sig domain.Signal, payload []byte) error
}

// Querier answers the read-side questions the UI asks.
type Querier interface {
	Services(ctx context.Context, r domain.TimeRange) ([]domain.ServiceSummary, error)
	Operations(ctx context.Context, service string, r domain.TimeRange) ([]domain.OperationSummary, error)
	SearchLogs(ctx context.Context, q domain.LogQuery) (domain.LogPage, error)
	LogHistogram(ctx context.Context, q domain.LogQuery) (domain.Histogram, error)
	SearchTraces(ctx context.Context, q domain.TraceQuery) ([]domain.TraceSummary, error)
	Trace(ctx context.Context, id [16]byte) (domain.Trace, error)
}

// MetricsQuerier answers time-series questions for charts.
type MetricsQuerier interface {
	MetricNames(ctx context.Context, service string, r domain.TimeRange) ([]domain.MetricName, error)
	QueryMetric(ctx context.Context, q domain.MetricQuery, r domain.TimeRange) (domain.MetricResult, error)
	ServiceSeries(ctx context.Context, service string, r domain.TimeRange) (time.Duration, []domain.REDPoint, error)
}

type DashboardManager interface {
	ListDashboards(ctx context.Context) ([]domain.Dashboard, error)
	GetDashboard(ctx context.Context, id string) (domain.Dashboard, error)
	// SaveDashboard creates the dashboard when ID is empty, else replaces it.
	SaveDashboard(ctx context.Context, d domain.Dashboard) (domain.Dashboard, error)
	DeleteDashboard(ctx context.Context, id string) error
}

type AlertManager interface {
	ListRules(ctx context.Context) ([]domain.AlertRule, error)
	GetRule(ctx context.Context, id string) (domain.AlertRule, error)
	SaveRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error)
	DeleteRule(ctx context.Context, id string) error
	Statuses(ctx context.Context) (map[string]domain.AlertStatus, error)
	Events(ctx context.Context, ruleID string, limit int) ([]domain.AlertEvent, error)
	// TestRule sends a test notification for the rule.
	TestRule(ctx context.Context, id string) error
}

// Authenticator signs users in and resolves sessions.
type Authenticator interface {
	// Login checks credentials (with lockout) and returns a session token.
	Login(ctx context.Context, username, password, clientIP string) (token string, u domain.User, err error)
	// Authenticate resolves a session token to its user.
	Authenticate(ctx context.Context, token string) (domain.User, error)
	// Logout ends every session of the user.
	Logout(ctx context.Context, userID string) error
	ChangePassword(ctx context.Context, userID, current, next string) error
}

// UserManager is for admins.
type UserManager interface {
	ListUsers(ctx context.Context) ([]domain.User, error)
	CreateUser(ctx context.Context, username string, role domain.Role, password string) (domain.User, error)
	// UpdateUser changes role and disabled state; a non-empty password resets it.
	UpdateUser(ctx context.Context, actorID, id string, role domain.Role, disabled bool, password string) (domain.User, error)
	DeleteUser(ctx context.Context, actorID, id string) error
}

// BatchWriter buffers decoded telemetry and persists it on Flush.
// It is driven by the queue consumer, which commits offsets only after a
// successful Flush.
type BatchWriter interface {
	Add(b domain.Batch, now time.Time)
	// Buffered returns the number of rows waiting to be written.
	Buffered() int
	// Flush writes everything that is ready. final also flushes rows that are
	// still being held back (such as traces awaiting a sampling decision).
	// A failed Flush keeps the rows that were not written, so it can be retried.
	Flush(ctx context.Context, now time.Time, final bool) error
}
