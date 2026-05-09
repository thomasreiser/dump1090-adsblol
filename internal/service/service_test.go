package service

import (
	"context"
	"os"
	"testing"
	"time"

	adsbv2 "github.com/adsblol/dump1090-adsblol/gen/adsb/v2"
	"github.com/adsblol/dump1090-adsblol/internal/aircraftdb"
	"github.com/adsblol/dump1090-adsblol/internal/filters"
	"github.com/adsblol/dump1090-adsblol/internal/sbs"
	"github.com/adsblol/dump1090-adsblol/internal/tracker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ptrStr(s string) *string   { return &s }
func ptrI32(v int32) *int32     { return &v }
func ptrF64(v float64) *float64 { return &v }
func ptrBool(b bool) *bool      { return &b }

// bootstrap seeds a tracker with a fixed cohort and returns a Server backed by
// it. csvContents is loaded as the aircraft DB (use "" for an empty DB).
func bootstrap(t *testing.T, csvContents string) (*tracker.Tracker, *Server) {
	t.Helper()

	tk := tracker.New(time.Minute)
	clock := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tk.WithClock(func() time.Time { return clock })

	// AAL1 over NYC.
	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "a1b2c3", Callsign: ptrStr("AAL1")})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "a1b2c3", Altitude: ptrI32(35000), Latitude: ptrF64(40.7128), Longitude: ptrF64(-74.0060)})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirborneVelocity, HexIdent: "a1b2c3", GroundSpeed: ptrF64(450), Track: ptrF64(90), VerticalRate: ptrI32(-256)})
	tk.Apply(sbs.Message{Type: sbs.MsgSurveillanceID, HexIdent: "a1b2c3", Squawk: ptrStr("1234")})
	// UAL2 over LAX, squawking 7700.
	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "def456", Callsign: ptrStr("UAL2")})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "def456", Altitude: ptrI32(38000), Latitude: ptrF64(33.9425), Longitude: ptrF64(-118.4081)})
	tk.Apply(sbs.Message{Type: sbs.MsgSurveillanceID, HexIdent: "def456", Squawk: ptrStr("7700"), Emergency: ptrBool(true)})
	// US mil hex over LA.
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "ae1234", Altitude: ptrI32(20000), Latitude: ptrF64(34.0), Longitude: ptrF64(-118.0)})
	// PIA hex.
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "adf001", Altitude: ptrI32(10000), Latitude: ptrF64(40.0), Longitude: ptrF64(-75.0)})
	// Callsign-only, no position — should be invisible to radius queries.
	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "c01a2b", Callsign: ptrStr("KLM3")})

	db := aircraftdb.Empty()
	if csvContents != "" {
		path := writeTempCSV(t, csvContents)
		loaded, err := aircraftdb.LoadCSV(path)
		if err != nil {
			t.Fatal(err)
		}
		db = loaded
	}

	srv := New(tk, db, filters.DefaultMil(), filters.NewHexSet())
	return tk, srv
}

func writeTempCSV(t *testing.T, contents string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ac-*.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestGetByHex(t *testing.T) {
	_, srv := bootstrap(t, "a1b2c3,N123AA,A320\ndef456,N456UA,B738\n")

	resp, err := srv.GetByHex(context.Background(), &adsbv2.HexRequest{HexList: "a1b2c3"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1", resp.Total)
	}
	a := resp.Ac[0]
	if a.Hex != "a1b2c3" || a.Flight != "AAL1" {
		t.Errorf("aircraft = %+v", a)
	}
	if a.R != "N123AA" || a.T != "A320" {
		t.Errorf("DB enrichment missing: r=%q t=%q", a.R, a.T)
	}
	if a.AltBaro != 35000 || a.Gs != 450 {
		t.Errorf("altitude/gs lost: %+v", a)
	}
	if a.Squawk != "1234" {
		t.Errorf("squawk = %q", a.Squawk)
	}
	if resp.Msg != "No error" || resp.Now == 0 {
		t.Errorf("envelope bad: %+v", resp)
	}
}

func TestGetByHex_MultiAndMissing(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetByHex(context.Background(), &adsbv2.HexRequest{HexList: "A1B2C3,def456,000000"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Fatalf("total = %d, want 2", resp.Total)
	}
}

func TestGetByHex_EmptyList(t *testing.T) {
	_, srv := bootstrap(t, "")
	_, err := srv.GetByHex(context.Background(), &adsbv2.HexRequest{HexList: ""})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", got)
	}
}

func TestGetByCallsign(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetByCallsign(context.Background(), &adsbv2.CallsignRequest{CallsignList: "aal1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Flight != "AAL1" {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestGetByRegistration(t *testing.T) {
	_, srv := bootstrap(t, "a1b2c3,N123AA,A320\ndef456,N456UA,B738\n")
	resp, err := srv.GetByRegistration(context.Background(), &adsbv2.RegistrationRequest{RegList: "n123aa"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Hex != "a1b2c3" {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestGetByIcaoType(t *testing.T) {
	_, srv := bootstrap(t, "a1b2c3,N123AA,A320\ndef456,N456UA,B738\n")
	resp, err := srv.GetByIcaoType(context.Background(), &adsbv2.IcaoTypeRequest{TypeList: "a320,B738"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
}

func TestGetBySquawk(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetBySquawk(context.Background(), &adsbv2.SquawkRequest{Squawk: "7700"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Hex != "def456" {
		t.Errorf("unexpected: %+v", resp)
	}
	if resp.Ac[0].Emergency != "general" {
		t.Errorf("emergency = %q", resp.Ac[0].Emergency)
	}
}

func TestGetMilitary(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetMilitary(context.Background(), &adsbv2.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Hex != "ae1234" {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestGetPia(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetPia(context.Background(), &adsbv2.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Hex != "adf001" {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestGetLadd_EmptyByDefault(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetLadd(context.Background(), &adsbv2.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Errorf("default LADD should be empty, got %d", resp.Total)
	}
}

func TestGetWithinRadius(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetWithinRadius(context.Background(), &adsbv2.RadiusRequest{
		Lat: 40.7128, Lon: -74.0060, Dist: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || resp.Ac[0].Hex != "a1b2c3" {
		t.Errorf("got %d, hex %v", resp.Total, resp.Ac)
	}
	if resp.Ac[0].Dst < 0 || resp.Ac[0].Dst > 50 {
		t.Errorf("dst = %v, expected NM value in [0, 50]", resp.Ac[0].Dst)
	}
}

func TestGetWithinRadius_ValidatesInputs(t *testing.T) {
	_, srv := bootstrap(t, "")
	cases := []*adsbv2.RadiusRequest{
		{Lat: 91, Lon: 0, Dist: 10},
		{Lat: 0, Lon: 200, Dist: 10},
		{Lat: 0, Lon: 0, Dist: 0},
		{Lat: 0, Lon: 0, Dist: 500},
	}
	for _, c := range cases {
		if _, err := srv.GetWithinRadius(context.Background(), c); status.Code(err) != codes.InvalidArgument {
			t.Errorf("input %+v: expected InvalidArgument, got %v", c, err)
		}
	}
}

func TestGetClosest(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetClosest(context.Background(), &adsbv2.RadiusRequest{
		Lat: 34.0, Lon: -118.2, Dist: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1", resp.Total)
	}
	if resp.Ac[0].Hex != "ae1234" {
		t.Errorf("closest = %s, want ae1234", resp.Ac[0].Hex)
	}
}

func TestGetClosest_NoneInRange(t *testing.T) {
	_, srv := bootstrap(t, "")
	resp, err := srv.GetClosest(context.Background(), &adsbv2.RadiusRequest{
		Lat: 0, Lon: 0, Dist: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Errorf("expected empty, got %d", resp.Total)
	}
}
