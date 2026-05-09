// Package tracker maintains an in-memory snapshot of all aircraft currently
// being heard via dump1090, merging SBS messages into per-hex state.
package tracker

import (
	"strings"
	"sync"
	"time"

	"github.com/adsblol/dump1090-adsblol/internal/sbs"
)

// State is the merged set of fields we've ever observed for a single hex.
// Pointer scalars distinguish unknown from zero.
type State struct {
	Hex          string
	Callsign     string
	Altitude     *int32
	GroundSpeed  *float64
	Track        *float64
	Latitude     *float64
	Longitude    *float64
	VerticalRate *int32
	Squawk       string
	Alert        bool
	Emergency    bool
	SPI          bool
	OnGround     bool

	FirstSeen   time.Time
	LastSeen    time.Time
	LastPosSeen time.Time
	Messages    int64
}

// HasPosition reports whether we have ever seen a lat/lon for this aircraft.
func (s *State) HasPosition() bool {
	return s.Latitude != nil && s.Longitude != nil
}

// Tracker is the goroutine-safe state store.
type Tracker struct {
	mu          sync.RWMutex
	byHex       map[string]*State
	now         func() time.Time
	maxAge      time.Duration
	msgReceived int64
}

// Stats returns a snapshot of tracker counters for diagnostic logging.
type Stats struct {
	Aircraft    int
	MsgReceived int64
}

// Stats returns current tracker counters.
func (t *Tracker) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Stats{Aircraft: len(t.byHex), MsgReceived: t.msgReceived}
}

// New returns a tracker with the given staleness window.
func New(maxAge time.Duration) *Tracker {
	return &Tracker{
		byHex:  make(map[string]*State),
		now:    time.Now,
		maxAge: maxAge,
	}
}

// WithClock injects a clock for tests.
func (t *Tracker) WithClock(now func() time.Time) *Tracker {
	t.now = now
	return t
}

// Apply merges one SBS message into the per-hex state.
func (t *Tracker) Apply(m sbs.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	s, ok := t.byHex[m.HexIdent]
	if !ok {
		s = &State{Hex: m.HexIdent, FirstSeen: now}
		t.byHex[m.HexIdent] = s
	}
	s.LastSeen = now
	s.Messages++
	t.msgReceived++

	if m.Callsign != nil {
		s.Callsign = *m.Callsign
	}
	if m.Altitude != nil {
		v := *m.Altitude
		s.Altitude = &v
	}
	if m.GroundSpeed != nil {
		v := *m.GroundSpeed
		s.GroundSpeed = &v
	}
	if m.Track != nil {
		v := *m.Track
		s.Track = &v
	}
	if m.Latitude != nil && m.Longitude != nil {
		la, lo := *m.Latitude, *m.Longitude
		s.Latitude = &la
		s.Longitude = &lo
		s.LastPosSeen = now
	}
	if m.VerticalRate != nil {
		v := *m.VerticalRate
		s.VerticalRate = &v
	}
	if m.Squawk != nil {
		s.Squawk = *m.Squawk
	}
	if m.Alert != nil {
		s.Alert = *m.Alert
	}
	if m.Emergency != nil {
		s.Emergency = *m.Emergency
	}
	if m.SPI != nil {
		s.SPI = *m.SPI
	}
	if m.OnGround != nil {
		s.OnGround = *m.OnGround
	}
}

// Snapshot returns a copy of all currently-tracked states.
func (t *Tracker) Snapshot() []State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]State, 0, len(t.byHex))
	for _, s := range t.byHex {
		out = append(out, *s)
	}
	return out
}

// SnapshotFilter returns a copy of states for which keep returns true.
func (t *Tracker) SnapshotFilter(keep func(*State) bool) []State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]State, 0, len(t.byHex))
	for _, s := range t.byHex {
		if keep(s) {
			out = append(out, *s)
		}
	}
	return out
}

// Get returns the state for one hex if present.
func (t *Tracker) Get(hex string) (State, bool) {
	hex = strings.ToLower(hex)
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.byHex[hex]
	if !ok {
		return State{}, false
	}
	return *s, true
}

// Now exposes the tracker's clock; service handlers use this so /now in
// responses matches the timestamps in the underlying snapshot.
func (t *Tracker) Now() time.Time { return t.now() }

// EvictBefore drops any aircraft whose LastSeen is older than the tracker's
// maxAge relative to the current clock. Returns the number of entries removed.
func (t *Tracker) EvictBefore() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := t.now().Add(-t.maxAge)
	n := 0
	for hex, s := range t.byHex {
		if s.LastSeen.Before(cutoff) {
			delete(t.byHex, hex)
			n++
		}
	}
	return n
}

// RunEviction sweeps stale entries every `every` until ctx is done. Intended
// to be launched as a goroutine.
func (t *Tracker) RunEviction(done <-chan struct{}, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			t.EvictBefore()
		}
	}
}
