package policy

import "time"

// Seen remembers recently written dimension IDs (resources, metric series)
// so each is inserted once per TTL instead of once per row. Rewriting after
// TTL refreshes last_seen, which keeps the row alive in ClickHouse.
type Seen struct {
	ttl time.Duration
	max int
	m   map[uint64]time.Time
}

func NewSeen(ttl time.Duration, max int) *Seen {
	return &Seen{ttl: ttl, max: max, m: map[uint64]time.Time{}}
}

// ShouldWrite reports whether id needs to be (re)written and marks it written.
func (s *Seen) ShouldWrite(id uint64, now time.Time) bool {
	if t, ok := s.m[id]; ok && now.Sub(t) < s.ttl {
		return false
	}
	if len(s.m) >= s.max {
		clear(s.m)
	}
	s.m[id] = now
	return true
}
