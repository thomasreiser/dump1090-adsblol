// Package service implements the adsb.v2.AdsbService gRPC interface backed by
// the in-memory tracker. The HTTP shim is provided by grpc-gateway against
// these same methods.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adsbv2 "github.com/adsblol/dump1090-adsblol/gen/adsb/v2"
	"github.com/adsblol/dump1090-adsblol/internal/aircraftdb"
	"github.com/adsblol/dump1090-adsblol/internal/filters"
	"github.com/adsblol/dump1090-adsblol/internal/geo"
	"github.com/adsblol/dump1090-adsblol/internal/tracker"
)

// Server implements adsbv2.AdsbServiceServer.
type Server struct {
	adsbv2.UnimplementedAdsbServiceServer

	Tracker *tracker.Tracker
	DB      *aircraftdb.DB
	Mil     *filters.HexSet
	Ladd    *filters.HexSet
}

// New creates a Server with the given backing stores. mil/ladd may be nil, in
// which case empty sets are used.
func New(t *tracker.Tracker, db *aircraftdb.DB, mil, ladd *filters.HexSet) *Server {
	if db == nil {
		db = aircraftdb.Empty()
	}
	if mil == nil {
		mil = filters.NewHexSet()
	}
	if ladd == nil {
		ladd = filters.NewHexSet()
	}
	return &Server{Tracker: t, DB: db, Mil: mil, Ladd: ladd}
}

// --- gRPC handlers ---

func (s *Server) GetByHex(ctx context.Context, req *adsbv2.HexRequest) (*adsbv2.V2Response, error) {
	hexes, err := splitList(req.GetHexList(), 6, "hex")
	if err != nil {
		return nil, err
	}
	start := time.Now()
	wanted := map[string]struct{}{}
	for _, h := range hexes {
		wanted[strings.ToLower(h)] = struct{}{}
	}
	matched := s.snapshotFilter(func(st *tracker.State) bool {
		_, ok := wanted[st.Hex]
		return ok
	})
	return s.respond(matched, start), nil
}

func (s *Server) GetByCallsign(ctx context.Context, req *adsbv2.CallsignRequest) (*adsbv2.V2Response, error) {
	cs, err := splitList(req.GetCallsignList(), 0, "callsign")
	if err != nil {
		return nil, err
	}
	start := time.Now()
	wanted := map[string]struct{}{}
	for _, c := range cs {
		wanted[strings.ToUpper(strings.TrimSpace(c))] = struct{}{}
	}
	matched := s.snapshotFilter(func(st *tracker.State) bool {
		_, ok := wanted[strings.ToUpper(strings.TrimSpace(st.Callsign))]
		return ok && st.Callsign != ""
	})
	return s.respond(matched, start), nil
}

func (s *Server) GetByRegistration(ctx context.Context, req *adsbv2.RegistrationRequest) (*adsbv2.V2Response, error) {
	regs, err := splitList(req.GetRegList(), 0, "registration")
	if err != nil {
		return nil, err
	}
	start := time.Now()
	wanted := map[string]struct{}{}
	for _, r := range regs {
		for _, h := range s.DB.HexesByRegistration(r) {
			wanted[h] = struct{}{}
		}
	}
	matched := s.snapshotFilter(func(st *tracker.State) bool {
		_, ok := wanted[st.Hex]
		return ok
	})
	return s.respond(matched, start), nil
}

func (s *Server) GetByIcaoType(ctx context.Context, req *adsbv2.IcaoTypeRequest) (*adsbv2.V2Response, error) {
	types, err := splitList(req.GetTypeList(), 0, "type")
	if err != nil {
		return nil, err
	}
	start := time.Now()
	wanted := map[string]struct{}{}
	for _, t := range types {
		for _, h := range s.DB.HexesByType(t) {
			wanted[h] = struct{}{}
		}
	}
	matched := s.snapshotFilter(func(st *tracker.State) bool {
		_, ok := wanted[st.Hex]
		return ok
	})
	return s.respond(matched, start), nil
}

func (s *Server) GetBySquawk(ctx context.Context, req *adsbv2.SquawkRequest) (*adsbv2.V2Response, error) {
	sq := strings.TrimSpace(req.GetSquawk())
	if sq == "" {
		return nil, status.Error(codes.InvalidArgument, "squawk is required")
	}
	start := time.Now()
	matched := s.snapshotFilter(func(st *tracker.State) bool {
		return st.Squawk == sq
	})
	return s.respond(matched, start), nil
}

func (s *Server) GetMilitary(ctx context.Context, _ *adsbv2.EmptyRequest) (*adsbv2.V2Response, error) {
	start := time.Now()
	matched := s.snapshotFilter(func(st *tracker.State) bool { return s.Mil.Contains(st.Hex) })
	return s.respond(matched, start), nil
}

func (s *Server) GetLadd(ctx context.Context, _ *adsbv2.EmptyRequest) (*adsbv2.V2Response, error) {
	start := time.Now()
	matched := s.snapshotFilter(func(st *tracker.State) bool { return s.Ladd.Contains(st.Hex) })
	return s.respond(matched, start), nil
}

func (s *Server) GetPia(ctx context.Context, _ *adsbv2.EmptyRequest) (*adsbv2.V2Response, error) {
	start := time.Now()
	matched := s.snapshotFilter(func(st *tracker.State) bool { return filters.IsPIA(st.Hex) })
	return s.respond(matched, start), nil
}

func (s *Server) GetWithinRadius(ctx context.Context, req *adsbv2.RadiusRequest) (*adsbv2.V2Response, error) {
	if err := validateRadius(req); err != nil {
		return nil, err
	}
	start := time.Now()
	matched, distances := s.withinRadius(req.GetLat(), req.GetLon(), req.GetDist())
	resp := s.respond(matched, start)
	for i := range resp.Ac {
		resp.Ac[i].Dst = distances[i]
	}
	return resp, nil
}

func (s *Server) GetClosest(ctx context.Context, req *adsbv2.RadiusRequest) (*adsbv2.V2Response, error) {
	if err := validateRadius(req); err != nil {
		return nil, err
	}
	start := time.Now()
	matched, distances := s.withinRadius(req.GetLat(), req.GetLon(), req.GetDist())
	if len(matched) == 0 {
		return s.respond(matched, start), nil
	}
	bestIdx := 0
	for i, d := range distances {
		if d < distances[bestIdx] {
			bestIdx = i
		}
	}
	matched = matched[bestIdx : bestIdx+1]
	best := distances[bestIdx]
	resp := s.respond(matched, start)
	resp.Ac[0].Dst = best
	return resp, nil
}

// --- helpers ---

func (s *Server) withinRadius(lat, lon, distNM float64) ([]tracker.State, []float64) {
	all := s.Tracker.SnapshotFilter(func(st *tracker.State) bool { return st.HasPosition() })
	out := make([]tracker.State, 0, len(all))
	dists := make([]float64, 0, len(all))
	for _, st := range all {
		d := geo.DistanceNM(lat, lon, *st.Latitude, *st.Longitude)
		if d <= distNM {
			out = append(out, st)
			dists = append(dists, d)
		}
	}
	return out, dists
}

func (s *Server) snapshotFilter(keep func(*tracker.State) bool) []tracker.State {
	out := s.Tracker.SnapshotFilter(keep)
	sort.Slice(out, func(i, j int) bool { return out[i].Hex < out[j].Hex })
	return out
}

// respond builds a V2Response and converts states to protobuf Aircraft,
// enriching with the aircraft DB for `r` and `t`.
func (s *Server) respond(states []tracker.State, start time.Time) *adsbv2.V2Response {
	now := s.Tracker.Now()
	ms := float64(now.UnixMilli())
	resp := &adsbv2.V2Response{
		Msg:   "No error",
		Now:   ms,
		Ctime: ms,
		Total: int32(len(states)),
		Ac:    make([]*adsbv2.Aircraft, 0, len(states)),
	}
	for _, st := range states {
		resp.Ac = append(resp.Ac, s.toAircraft(&st, now))
	}
	resp.Ptime = float64(time.Since(start).Milliseconds())
	return resp
}

func (s *Server) toAircraft(st *tracker.State, now time.Time) *adsbv2.Aircraft {
	a := &adsbv2.Aircraft{
		Hex:      st.Hex,
		Type:     "adsb_icao",
		Flight:   st.Callsign,
		Squawk:   st.Squawk,
		Messages: float64(st.Messages),
		Ground:   st.OnGround,
	}
	if st.OnGround {
		a.AltBaro = 0
	} else if st.Altitude != nil {
		a.AltBaro = *st.Altitude
	}
	if st.GroundSpeed != nil {
		a.Gs = *st.GroundSpeed
	}
	if st.Track != nil {
		a.Track = *st.Track
	}
	if st.VerticalRate != nil {
		a.BaroRate = *st.VerticalRate
	}
	if st.Latitude != nil {
		a.Lat = *st.Latitude
	}
	if st.Longitude != nil {
		a.Lon = *st.Longitude
	}
	if st.Alert {
		a.Alert = 1
	}
	if st.SPI {
		a.Spi = 1
	}
	if st.Emergency {
		a.Emergency = "general"
	} else {
		a.Emergency = "none"
	}
	if !st.LastSeen.IsZero() {
		a.Seen = now.Sub(st.LastSeen).Seconds()
	}
	if !st.LastPosSeen.IsZero() {
		a.SeenPos = now.Sub(st.LastPosSeen).Seconds()
	}
	if e := s.DB.Lookup(st.Hex); e.Hex != "" {
		a.R = e.Registration
		a.T = e.IcaoType
	}
	return a
}

// splitList splits a comma-separated path parameter and validates that each
// non-empty entry has at most maxLen characters (or any length if maxLen==0).
// Returns codes.InvalidArgument if the list is empty.
func splitList(raw string, maxLen int, label string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s list is required", label)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if maxLen > 0 && len(p) > maxLen {
			return nil, status.Errorf(codes.InvalidArgument, "%s %q exceeds %d characters", label, p, maxLen)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "%s list is empty", label)
	}
	return out, nil
}

func validateRadius(req *adsbv2.RadiusRequest) error {
	switch {
	case req.GetLat() < -90 || req.GetLat() > 90:
		return status.Error(codes.InvalidArgument, "lat must be in [-90, 90]")
	case req.GetLon() < -180 || req.GetLon() > 180:
		return status.Error(codes.InvalidArgument, "lon must be in [-180, 180]")
	case req.GetDist() <= 0:
		return status.Error(codes.InvalidArgument, "dist must be > 0")
	case req.GetDist() > 250:
		return status.Error(codes.InvalidArgument, "dist must be <= 250 NM")
	}
	return nil
}

// Errors that the gateway can surface — kept for symmetry; callers use the
// status codes above directly.
var _ = errors.New
