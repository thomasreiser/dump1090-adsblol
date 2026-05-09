// Command server hosts the adsb.lol-compatible /v2 API backed by a local
// dump1090 BaseStation feed. gRPC and REST share one in-process service;
// REST is provided by grpc-gateway in handler-server mode (no extra hop)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	adsbv2 "github.com/adsblol/dump1090-adsblol/gen/adsb/v2"
	"github.com/adsblol/dump1090-adsblol/internal/aircraftdb"
	"github.com/adsblol/dump1090-adsblol/internal/docs"
	"github.com/adsblol/dump1090-adsblol/internal/filters"
	"github.com/adsblol/dump1090-adsblol/internal/sbs"
	"github.com/adsblol/dump1090-adsblol/internal/service"
	"github.com/adsblol/dump1090-adsblol/internal/tracker"
)

type config struct {
	dump1090   string
	grpcAddr   string
	httpAddr   string
	aircraftDB string
	milFile    string
	laddFile   string
	maxAge     time.Duration
	evictEvery time.Duration
	logLevel   string
}

func main() {
	cfg := parseFlags()
	logger := newLogger(cfg.logLevel)
	slog.SetDefault(logger)

	if err := run(cfg, logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.dump1090, "dump1090", "localhost:30003", "host:port of dump1090 BaseStation (SBS) TCP stream")
	flag.StringVar(&cfg.grpcAddr, "grpc", ":50051", "gRPC listen address")
	flag.StringVar(&cfg.httpAddr, "http", ":8080", "HTTP/REST listen address")
	flag.StringVar(&cfg.aircraftDB, "aircraft-db", "", "optional CSV file with hex,registration,icaotype rows")
	flag.StringVar(&cfg.milFile, "mil-file", "", "optional file with military hex codes (one per line; ae0000-afffff range allowed)")
	flag.StringVar(&cfg.laddFile, "ladd-file", "", "optional file with LADD hex codes")
	flag.DurationVar(&cfg.maxAge, "max-age", 60*time.Second, "drop aircraft not seen for this long")
	flag.DurationVar(&cfg.evictEvery, "evict-every", 10*time.Second, "stale-entry sweep interval")
	flag.StringVar(&cfg.logLevel, "log-level", "info", "debug|info|warn|error")
	flag.Parse()
	return cfg
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

func run(cfg config, log *slog.Logger) error {
	db, err := aircraftdb.LoadCSV(cfg.aircraftDB)
	if err != nil {
		return fmt.Errorf("load aircraft db: %w", err)
	}
	if cfg.aircraftDB != "" {
		log.Info("aircraft db loaded", "path", cfg.aircraftDB)
	}

	mil, err := loadHexSetOrDefault(cfg.milFile, filters.DefaultMil)
	if err != nil {
		return fmt.Errorf("load mil hex set: %w", err)
	}
	if cfg.milFile != "" {
		log.Info("mil hex set loaded from file", "path", cfg.milFile)
	} else {
		log.Info("mil hex set using default US block ae0000-afffff")
	}

	ladd, err := filters.LoadHexFile(cfg.laddFile)
	if err != nil {
		return fmt.Errorf("load ladd hex set: %w", err)
	}
	if cfg.laddFile != "" {
		log.Info("ladd hex set loaded from file", "path", cfg.laddFile)
	}

	tk := tracker.New(cfg.maxAge)
	svc := service.New(tk, db, mil, ladd)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		client := &sbs.Client{
			Addr:      cfg.dump1090,
			OnMessage: tk.Apply,
			Logger:    log.With("component", "sbs"),
		}
		if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("sbs client exited", "err", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		tk.RunEviction(ctx.Done(), cfg.evictEvery)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s := tk.Stats()
				log.Info("tracker stats", "aircraft", s.Aircraft, "messages_received", s.MsgReceived)
			}
		}
	}()

	grpcServer := grpc.NewServer()
	adsbv2.RegisterAdsbServiceServer(grpcServer, svc)

	grpcListener, err := net.Listen("tcp", cfg.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.grpcAddr, err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("gRPC listening", "addr", cfg.grpcAddr)
		if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Error("grpc serve", "err", err)
			cancel()
		}
	}()

	gatewayMux := runtime.NewServeMux(
		// adsb.lol uses snake_case in JSON; protojson defaults to lowerCamel
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
	)
	if err := adsbv2.RegisterAdsbServiceHandlerServer(ctx, gatewayMux, svc); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	httpMux.Handle("/openapi.json", docs.SpecHandler())
	httpMux.Handle("/docs", http.RedirectHandler("/docs/", http.StatusMovedPermanently))
	httpMux.Handle("/docs/", http.StripPrefix("/docs", docs.SwaggerUIHandler("/openapi.json")))
	httpMux.HandleFunc("/debug/aircraft", func(w http.ResponseWriter, _ *http.Request) {
		debugDumpAircraft(w, tk)
	})
	httpMux.Handle("/", gatewayMux)

	httpServer := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("HTTP listening", "addr", cfg.httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http serve", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown requested")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpServer.Shutdown(shutCtx)
	grpcServer.GracefulStop()

	wg.Wait()
	log.Info("server stopped")
	return nil
}

func loadHexSetOrDefault(path string, def func() *filters.HexSet) (*filters.HexSet, error) {
	if path == "" {
		return def(), nil
	}
	return filters.LoadHexFile(path)
}

// debugDumpAircraft writes every tracked aircraft, unfiltered, as JSON. Pick
// a hex from here and feed it to /v2/hex to sanity-check the real pipeline
func debugDumpAircraft(w http.ResponseWriter, tk *tracker.Tracker) {
	type row struct {
		Hex      string   `json:"hex"`
		Flight   string   `json:"flight,omitempty"`
		Lat      *float64 `json:"lat,omitempty"`
		Lon      *float64 `json:"lon,omitempty"`
		Altitude *int32   `json:"alt_baro,omitempty"`
		Squawk   string   `json:"squawk,omitempty"`
		Messages int64    `json:"messages"`
		SeenSec  float64  `json:"seen_sec"`
	}
	now := tk.Now()
	states := tk.Snapshot()
	rows := make([]row, 0, len(states))
	for _, s := range states {
		rows = append(rows, row{
			Hex: s.Hex, Flight: s.Callsign, Lat: s.Latitude, Lon: s.Longitude,
			Altitude: s.Altitude, Squawk: s.Squawk, Messages: s.Messages,
			SeenSec: now.Sub(s.LastSeen).Seconds(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Total int   `json:"total"`
		Ac    []row `json:"ac"`
	}{Total: len(rows), Ac: rows})
}
