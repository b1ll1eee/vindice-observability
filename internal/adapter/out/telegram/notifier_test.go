package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
)

const token = "123456:SECRET-token"

var rule = domain.AlertRule{
	Name:   "orders <errors>",
	Signal: domain.SignalQuery{Kind: domain.SignalKindRED, Service: "orders", Measure: domain.MeasureErrorRate},
	Window: 5 * time.Minute, Op: domain.OpGT, Threshold: 0.05,
}

func TestNotifySendsHTML(t *testing.T) {
	var got sendMessage
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := New(srv.URL, token, "-1001")
	err := n.Notify(context.Background(), domain.Notification{
		Event: domain.EventFiring, Rule: rule, Value: 0.123456, Breaching: []string{"orders = 0.12"},
		URL: "http://vindice/alerts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/bot"+token+"/sendMessage" || got.ChatID != "-1001" || got.ParseMode != "HTML" {
		t.Errorf("path %s, message %+v", path, got)
	}
	for _, want := range []string{
		"🔥 <b>FIRING</b>: orders &lt;errors&gt;",
		"error_rate of orders &gt; 0.05 over 5m",
		"Value: <b>0.1235</b>",
		"• orders = 0.12",
		`<a href="http://vindice/alerts">`,
	} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("message missing %q:\n%s", want, got.Text)
		}
	}
}

func TestNotifyRuleChatOverridesDefault(t *testing.T) {
	var got sendMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	r := rule
	r.ChatID = "42"
	_ = New(srv.URL, token, "-1001").Notify(context.Background(), domain.Notification{Event: domain.EventTest, Rule: r})
	if got.ChatID != "42" {
		t.Errorf("chat = %s", got.ChatID)
	}
	if err := New(srv.URL, token, "").Notify(context.Background(), domain.Notification{Rule: rule}); err == nil {
		t.Error("missing chat id should fail")
	}
}

func TestNotifyRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Too Many Requests","parameters":{"retry_after":7}}`))
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	n := New(srv.URL, token, "1")
	var waits []time.Duration
	n.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if err := n.Notify(context.Background(), domain.Notification{Rule: rule}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(waits) != 2 || waits[0] != 7*time.Second || waits[1] != time.Second {
		t.Errorf("calls %d, waits %v", calls, waits)
	}
}

func TestNotifyDoesNotRetryClientErrorsOrLeakToken(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()
	err := New(srv.URL, token, "1").Notify(context.Background(), domain.Notification{Rule: rule})
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("err %v, calls %d", err, calls)
	}

	// Transport errors include the URL, which contains the token.
	n := New("http://127.0.0.1:1", token, "1")
	n.sleep = func(context.Context, time.Duration) error { return nil }
	err = n.Notify(context.Background(), domain.Notification{Rule: rule})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("token leaked or no error: %v", err)
	}
}
