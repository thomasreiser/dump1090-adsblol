package service

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	adsbv2 "github.com/adsblol/dump1090-adsblol/gen/adsb/v2"
	"github.com/adsblol/dump1090-adsblol/internal/aircraftdb"
	"github.com/adsblol/dump1090-adsblol/internal/filters"
	"github.com/adsblol/dump1090-adsblol/internal/sbs"
	"github.com/adsblol/dump1090-adsblol/internal/tracker"
)

// TestE2E_GRPCAndREST stands up the real gRPC server and the grpc-gateway HTTP
// shim against a freshly-seeded tracker, then verifies one round-trip on each
// transport against the same endpoint to confirm the wiring works.
func TestE2E_GRPCAndREST(t *testing.T) {
	tk := tracker.New(time.Minute)
	clock := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tk.WithClock(func() time.Time { return clock })

	tk.Apply(sbs.Message{Type: sbs.MsgESIdentification, HexIdent: "a1b2c3", Callsign: ptrStr("AAL1")})
	tk.Apply(sbs.Message{Type: sbs.MsgESAirbornePosition, HexIdent: "a1b2c3", Altitude: ptrI32(35000), Latitude: ptrF64(40.7128), Longitude: ptrF64(-74.0060)})

	srv := New(tk, aircraftdb.Empty(), filters.DefaultMil(), filters.NewHexSet())

	// gRPC
	gs := grpc.NewServer()
	adsbv2.RegisterAdsbServiceServer(gs, srv)
	gln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go gs.Serve(gln)
	defer gs.Stop()

	cc, err := grpc.NewClient(gln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	client := adsbv2.NewAdsbServiceClient(cc)

	gresp, err := client.GetByHex(context.Background(), &adsbv2.HexRequest{HexList: "a1b2c3"})
	if err != nil {
		t.Fatalf("grpc GetByHex: %v", err)
	}
	if gresp.Total != 1 || gresp.Ac[0].Hex != "a1b2c3" {
		t.Fatalf("grpc response: %+v", gresp)
	}

	// REST (grpc-gateway, server-side handler mode)
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
		}),
	)
	if err := adsbv2.RegisterAdsbServiceHandlerServer(context.Background(), mux, srv); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v2/hex/a1b2c3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"alt_baro"`) {
		t.Errorf("expected snake_case field alt_baro in body: %s", body)
	}
	var decoded struct {
		Total int                      `json:"total"`
		Msg   string                   `json:"msg"`
		Ac    []map[string]interface{} `json:"ac"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if decoded.Total != 1 || decoded.Msg != "No error" || len(decoded.Ac) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.Ac[0]["hex"] != "a1b2c3" {
		t.Errorf("hex = %v", decoded.Ac[0]["hex"])
	}
}
