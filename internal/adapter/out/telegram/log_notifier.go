package telegram

import (
	"context"
	"log/slog"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

// LogNotifier stands in when no bot token is configured (Null Object): alerts
// still evaluate and are recorded, and the message goes to the log instead.
type LogNotifier struct{ Log *slog.Logger }

var _ port.Notifier = LogNotifier{}

func (l LogNotifier) Notify(_ context.Context, n domain.Notification) error {
	l.Log.Warn("alert (telegram not configured)", "event", n.Event, "rule", n.Rule.Name, "value", n.Value)
	return nil
}
