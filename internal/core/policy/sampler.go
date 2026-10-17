package policy

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

// Sampler decides which spans are stored (Strategy pattern).
type Sampler interface {
	// Add queues a span for a decision.
	Add(span domain.SpanRow, now time.Time)
	// Due returns the spans decided as kept. force decides everything pending.
	Due(now time.Time, force bool) []domain.SpanRow
	// Pending returns the number of spans still waiting for a decision.
	Pending() int
}

type SamplerConfig struct {
	Rate       float64       // fraction of normal traces kept, 0..1
	Slow       time.Duration // traces with a span at least this slow are kept
	Wait       time.Duration // how long to wait for the rest of a trace
	MaxPending int           // decide early when more spans than this are held
}

// NewSampler picks the strategy: keep everything at rate 1, tail sampling otherwise.
func NewSampler(c SamplerConfig) Sampler {
	if c.Rate >= 1 {
		return &keepAll{}
	}
	return &tailSampler{
		cfg:       c,
		threshold: uint64(max(c.Rate, 0) * math.MaxUint64),
		pending:   map[[16]byte]*pendingTrace{},
		decided:   map[[16]byte]decision{},
	}
}

type keepAll struct{ ready []domain.SpanRow }

func (k *keepAll) Add(span domain.SpanRow, _ time.Time) { k.ready = append(k.ready, span) }

func (k *keepAll) Due(time.Time, bool) []domain.SpanRow {
	out := k.ready
	k.ready = nil
	return out
}

func (k *keepAll) Pending() int { return 0 }

// tailSampler holds spans for Wait after the first span of a trace arrives;
// the whole trace is then kept if any span errored or was slower than Slow,
// or if the trace ID falls inside Rate.
//
// The ID check uses the random low bytes of the W3C trace ID, so every writer
// makes the same decision for the same trace. Spans arriving after a decision
// follow it. Held spans are lost if the process crashes (their Kafka offsets
// are already committed), which bounds the loss to at most Wait of traces.
type tailSampler struct {
	cfg       SamplerConfig
	threshold uint64
	pending   map[[16]byte]*pendingTrace
	nPending  int
	decided   map[[16]byte]decision
	ready     []domain.SpanRow
}

type pendingTrace struct {
	first time.Time
	keep  bool
	spans []domain.SpanRow
}

type decision struct {
	keep bool
	at   time.Time
}

func (s *tailSampler) Add(span domain.SpanRow, now time.Time) {
	if d, ok := s.decided[span.TraceID]; ok {
		if d.keep {
			s.ready = append(s.ready, span)
		}
		return
	}
	p := s.pending[span.TraceID]
	if p == nil {
		p = &pendingTrace{first: now}
		s.pending[span.TraceID] = p
	}
	if span.IsError() || (s.cfg.Slow > 0 && time.Duration(span.DurationNs) >= s.cfg.Slow) {
		p.keep = true
	}
	p.spans = append(p.spans, span)
	s.nPending++
}

func (s *tailSampler) Due(now time.Time, force bool) []domain.SpanRow {
	force = force || (s.cfg.MaxPending > 0 && s.nPending > s.cfg.MaxPending)
	for id, p := range s.pending {
		if !force && now.Sub(p.first) < s.cfg.Wait {
			continue
		}
		keep := p.keep || binary.BigEndian.Uint64(id[8:]) < s.threshold
		s.decided[id] = decision{keep: keep, at: now}
		if keep {
			s.ready = append(s.ready, p.spans...)
		}
		s.nPending -= len(p.spans)
		delete(s.pending, id)
	}
	// Late spans rarely arrive more than a few waits after the decision.
	for id, d := range s.decided {
		if now.Sub(d.at) > 6*s.cfg.Wait {
			delete(s.decided, id)
		}
	}
	out := s.ready
	s.ready = nil
	return out
}

func (s *tailSampler) Pending() int { return s.nPending }
