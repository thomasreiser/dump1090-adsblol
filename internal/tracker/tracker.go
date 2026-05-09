// Package tracker keeps an in-memory snapshot of all aircraft heard via SBS,
// merging incoming messages into a single per-hex state
package tracker

import (
	"strings"
	"sync"
	"time"

	"github.com/adsblol/dump1090-adsblol/internal/sbs"
)

// State is everything we've ever observed for one hex. Pointer scalars
// separate "unknown" from a real zero
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

func (s *State) HasPosition() bool {
	return s.Latitude != nil && s.Longitude != nil
}

type Tracker struct {
	mu          sync.RWMutex
	byHex       map[string]*State
	now         func() time.Time
	maxAge      time.Duration
	msgReceived int64
}

type Stats struct {
	Aircraft    int
	MsgReceived int64
}

func (t *Tracker) Stats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Stats{Aircraft: len(t.byHex), MsgReceived: t.msgReceived}
}

func New(maxAge time.Duration) *Tracker {
	return &Tracker{
		byHex:  make(map[string]*State),
		now:    time.Now,
		maxAge: maxAge,
	}
}

// WithClock swaps the clock — handy for deterministic tests
func (t *Tracker) WithClock(now func() time.Time) *Tracker {
	t.now = now
	return t
}

// Apply merges one SBS message into the per-hex state
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

func (t *Tracker) Snapshot() []State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]State, 0, len(t.byHex))
	for _, s := range t.byHex {
		out = append(out, *s)
	}
	return out
}

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

// Now exposes the tracker's clock so handlers stamp their responses with the
// same time that drove the snapshot
func (t *Tracker) Now() time.Time { return t.now() }

// EvictBefore drops every aircraft whose LastSeen is older than maxAge and
// returns how many it removed
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
