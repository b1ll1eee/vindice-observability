// Package telegram delivers alert notifications through the Telegram Bot API.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/b1ll1eee/vindice-observability/internal/core/domain"
	"github.com/b1ll1eee/vindice-observability/internal/core/port"
)

const DefaultAPI = "https://api.telegram.org"

type Notifier struct {
	api    string
	token  string
	chatID string
	client *http.Client
	// sleep is replaced in tests.
	sleep func(context.Context, time.Duration) error
}

var _ port.Notifier = (*Notifier)(nil)

// New returns a notifier for the bot token. chatID is used for rules that do
// not set their own. api is the Bot API base URL (DefaultAPI in production).
func New(api, token, chatID string) *Notifier {
	return &Notifier{
		api:    strings.TrimRight(api, "/"),
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 15 * time.Second},
		sleep:  sleepCtx,
	}
}

type sendMessage struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (n *Notifier) Notify(ctx context.Context, nt domain.Notification) error {
	chat := nt.Rule.ChatID
	if chat == "" {
		chat = n.chatID
	}
	if chat == "" {
		return errors.New("telegram: no chat id (set VINDICE_TELEGRAM_CHAT_ID or the rule's chat id)")
	}
	body, _ := json.Marshal(sendMessage{ChatID: chat, Text: Format(nt), ParseMode: "HTML", DisableWebPagePreview: true})

	backoff := time.Second
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		wait, err := n.send(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if wait == 0 { // not retryable
			break
		}
		if wait < 0 {
			wait, backoff = backoff, backoff*2
		}
		if err := n.sleep(ctx, min(wait, 30*time.Second)); err != nil {
			return err
		}
	}
	return lastErr
}

// send posts one message. wait > 0 asks the caller to retry after wait,
// wait < 0 to retry with backoff, wait == 0 not to retry.
func (n *Notifier) send(ctx context.Context, body []byte) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.api+"/bot"+n.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return 0, n.redact(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return -1, n.redact(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r apiResponse
	_ = json.Unmarshal(raw, &r)
	if resp.StatusCode == http.StatusOK && r.OK {
		return 0, nil
	}
	err = fmt.Errorf("telegram: %d %s", resp.StatusCode, r.Description)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return time.Duration(max(r.Parameters.RetryAfter, 1)) * time.Second, err
	case resp.StatusCode >= 500:
		return -1, err
	}
	return 0, err
}

// redact removes the bot token, which is part of every request URL, from
// errors so it never reaches logs or the UI.
func (n *Notifier) redact(err error) error {
	if n.token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), n.token, "<token>"))
}

// Format renders the notification as Telegram HTML.
func Format(n domain.Notification) string {
	var b strings.Builder
	switch n.Event {
	case domain.EventFiring:
		b.WriteString("🔥 <b>FIRING</b>: ")
	case domain.EventResolved:
		b.WriteString("✅ <b>RESOLVED</b>: ")
	default:
		b.WriteString("🧪 <b>TEST</b>: ")
	}
	b.WriteString(html.EscapeString(n.Rule.Name))
	fmt.Fprintf(&b, "\n%s %s %s over %s",
		html.EscapeString(describe(n.Rule.Signal)), html.EscapeString(string(n.Rule.Op)),
		formatNumber(n.Rule.Threshold), shortDuration(n.Rule.Window))
	fmt.Fprintf(&b, "\nValue: <b>%s</b>", formatNumber(n.Value))
	if len(n.Breaching) > 0 && n.Event != domain.EventResolved {
		b.WriteString("\n")
		for i, s := range n.Breaching {
			if i == 10 {
				fmt.Fprintf(&b, "\n… and %d more", len(n.Breaching)-10)
				break
			}
			b.WriteString("\n• " + html.EscapeString(s))
		}
	}
	if n.URL != "" {
		fmt.Fprintf(&b, "\n\n<a href=\"%s\">Open in Vindice</a>", html.EscapeString(n.URL))
	}
	return b.String()
}

func describe(s domain.SignalQuery) string {
	switch s.Kind {
	case domain.SignalKindMetric:
		d := fmt.Sprintf("%s(%s)", s.Metric.Agg, s.Metric.Name)
		if s.Metric.Service != "" {
			d += " of " + s.Metric.Service
		}
		return d
	case domain.SignalKindRED:
		return s.Measure + " of " + s.Service
	case domain.SignalKindLogs:
		d := "log count"
		if s.Service != "" {
			d += " of " + s.Service
		}
		if s.MinSeverity >= 17 {
			d += " (errors)"
		}
		if s.Text != "" {
			d += fmt.Sprintf(" matching %q", s.Text)
		}
		return d
	}
	return string(s.Kind)
}

// shortDuration prints 10m rather than 10m0s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func formatNumber(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
