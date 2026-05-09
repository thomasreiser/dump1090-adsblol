package tracker

import (
	"testing"
	"time"

	"github.com/adsblol/dump1090-adsblol/internal/sbs"
)

func ptrStr(s string) *string  { return &s }
func ptrI32(v int32) *int32    { return &v }
func ptrF64(v float64) *float64 { return &v }
func ptrBool(b bool) *bool     { return &b }

func TestApplyMergesFields(t *testing.T) {
	tk := New(time.Minute)
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tk.WithClock(func() time.Time { return now })

	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "abc123", Callsign: ptrStr("AAL1")})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "abc123", Altitude: ptrI32(35000), Latitude: ptrF64(40.0), Longitude: ptrF64(-74.0)})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirborneVelocity, HexIdent: "abc123", GroundSpeed: ptrF64(450), Track: ptrF64(180), VerticalRate: ptrI32(-512)})

	s, ok := tk.Get("abc123")
	if !ok {
		t.Fatal("hex missing")
	}
	if s.Callsign != "AAL1" {
		t.Errorf("callsign = %q", s.Callsign)
	}
	if s.Altitude == nil || *s.Altitude != 35000 {
		t.Errorf("altitude = %v", s.Altitude)
	}
	if !s.HasPosition() || *s.Latitude != 40.0 || *s.Longitude != -74.0 {
		t.Errorf("pos = %v,%v", s.Latitude, s.Longitude)
	}
	if s.Messages != 3 {
		t.Errorf("messages = %d, want 3", s.Messages)
	}
	if s.FirstSeen != now || s.LastSeen != now {
		t.Errorf("timestamps = %v / %v", s.FirstSeen, s.LastSeen)
	}
}

func TestApplyDoesNotClearKnownFields(t *testing.T) {
	tk := New(time.Minute)
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "abc123", Altitude: ptrI32(35000)})
	// a velocity-only message must not zero the previously-known altitude
	tk.Apply(sbs.Message{Type: sbs.MsgESAirborneVelocity, HexIdent: "abc123", GroundSpeed: ptrF64(400)})
	s, _ := tk.Get("abc123")
	if s.Altitude == nil || *s.Altitude != 35000 {
		t.Errorf("altitude lost: %v", s.Altitude)
	}
}

func TestEvictBefore(t *testing.T) {
	tk := New(10 * time.Second)
	clock := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tk.WithClock(func() time.Time { return clock })

	tk.Apply(sbs.Message{Type: sbs.MsgAllCallReply, HexIdent: "aaa111"})
	clock = clock.Add(20 * time.Second)
	tk.Apply(sbs.Message{Type: sbs.MsgAllCallReply, HexIdent: "bbb222"})

	if n := tk.EvictBefore(); n != 1 {
		t.Errorf("evicted = %d, want 1", n)
	}
	if _, ok := tk.Get("aaa111"); ok {
		t.Error("aaa111 should be evicted")
	}
	if _, ok := tk.Get("bbb222"); !ok {
		t.Error("bbb222 should still be present")
	}
}

func TestSnapshotFilter(t *testing.T) {
	tk := New(time.Minute)
	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "abc123", Callsign: ptrStr("AAL1")})
	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "def456", Callsign: ptrStr("UAL2")})

	got := tk.SnapshotFilter(func(s *State) bool { return s.Callsign == "AAL1" })
	if len(got) != 1 || got[0].Hex != "abc123" {
		t.Errorf("got = %+v", got)
	}
}
