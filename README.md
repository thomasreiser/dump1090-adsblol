# dump1090-adsblol

A small Go server that re-exposes a local [dump1090] receiver through the same
read-only `/v2` API shape as [adsb.lol]. It speaks both **gRPC** and **REST**
out of the same in-process service: REST is wired up by [grpc-gateway] so the
proto file is the single source of truth.

dump1090 is consumed via its BaseStation (SBS-1) TCP feed on port `30003`.
There's no positional-decoder or polling against `aircraft.json` — the server
maintains its own in-memory state machine from the SBS message stream.

## What works

| Endpoint | Method | Description |
| --- | --- | --- |
| `/v2/hex/{hex_list}` | `GetByHex` | Comma-separated 24-bit ICAO hex codes |
| `/v2/callsign/{callsign_list}` | `GetByCallsign` | Comma-separated callsigns |
| `/v2/reg/{reg_list}` | `GetByRegistration` | Needs an aircraft DB (see below) |
| `/v2/icao/{type_list}` (alias `/v2/type/...`) | `GetByIcaoType` | Needs an aircraft DB |
| `/v2/squawk/{squawk}` | `GetBySquawk` | 4-digit Mode A code |
| `/v2/mil` | `GetMilitary` | Defaults to US block `ae0000-afffff` |
| `/v2/ladd` | `GetLadd` | Empty unless a hex list is provided |
| `/v2/pia` | `GetPia` | Hex prefix `adf` |
| `/v2/lat/{lat}/lon/{lon}/dist/{dist}` | `GetWithinRadius` | `dist` is in nautical miles, capped at 250 |
| `/v2/closest/{lat}/{lon}/{dist}` | `GetClosest` | Single nearest aircraft |
| `/healthz` | – | Liveness probe |
| `/docs/` | – | Embedded Swagger UI (loads `/openapi.json`) |
| `/openapi.json` | – | OpenAPI v2 spec generated from the proto |

The response envelope mirrors adsb.lol's: `{ ac: [...], msg, now, total, ctime, ptime }`.

### Field-by-field coverage

The SBS BaseStation format is a strict subset of what adsb.lol normally
serves. Fields populated from SBS:

* `hex`, `flight`, `lat`, `lon`, `alt_baro`, `gs`, `track`, `baro_rate`,
  `squawk`, `alert`, `spi`, `emergency`, `ground`, `seen`, `seen_pos`,
  `messages`

Fields *only* populated if an aircraft DB is provided:

* `r` (registration), `t` (ICAO type designator)

Fields not derivable from SBS (always omitted): `alt_geom`, `ias`, `tas`,
`mach`, `roll`, `mag_heading`, `true_heading`, `geom_rate`, `category`,
`nic`, `rc`, `version`, `nic_baro`, `nac_p`, `nac_v`, `sil`, `sil_type`,
`gva`, `sda`, `rssi`, `mlat`, `tisb`.

> `alt_baro` is emitted as a plain integer, not the literal string `"ground"`
> that adsb.lol uses. When the aircraft is on the ground, `alt_baro` is `0`
> and the sibling `ground` field is `true`.

## Build & run

```sh
make tools   # one-time: installs protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway
make proto   # regenerates gen/
make build
make test
```

Run against a real dump1090 on the same host:

```sh
./server -dump1090=localhost:30003 -http=:8080 -grpc=:50051
```

With an aircraft DB and a military hex file:

```sh
./server \
  -dump1090=192.168.1.10:30003 \
  -aircraft-db=./data/aircraft.csv \
  -mil-file=./data/mil.txt
```

## Trying it without a receiver

A companion `sbs-feeder` ships canned SBS lines so you can smoke-test the
server with no hardware:

```sh
# terminal 1
go run ./cmd/sbs-feeder -addr=:30003

# terminal 2
go run ./cmd/server -dump1090=localhost:30003

# terminal 3
curl -s localhost:8080/v2/hex/a1b2c3 | jq .
curl -s "localhost:8080/v2/lat/40.7128/lon/-74.0060/dist/100" | jq .
curl -s localhost:8080/v2/squawk/7700 | jq .
```

## Optional data files

### Aircraft DB (`-aircraft-db`)

CSV with no header. Columns: `hex,registration,icao_type`. Lines starting
with `#` are skipped. Example:

```
a1b2c3,N123AA,A320
c01a2b,C-FXYZ,B738
```

The `r` and `t` response fields and the `/v2/reg` and `/v2/icao` endpoints
are powered exclusively by this file.

### Military / LADD hex sets (`-mil-file`, `-ladd-file`)

Plain text, one hex per line or a `lo-hi` inclusive range:

```
# US Navy specials
ae1234
# whole UK military block
43c000-43cfff
```

If `-mil-file` is omitted the server uses the conservative default of the
US military block `ae0000-afffff`. `-ladd-file` defaults to empty.

## Docker

```sh
make docker
docker run --rm -p 8080:8080 -p 50051:50051 \
  dump1090-adsblol:latest \
  -dump1090=host.docker.internal:30003
```

## Layout

```
cmd/
  server/       # main binary (gRPC + REST)
  sbs-feeder/   # canned-SBS server for local smoke testing
internal/
  sbs/          # SBS BaseStation parser + reconnecting TCP client
  tracker/      # per-hex in-memory state
  aircraftdb/   # CSV-loaded hex→{reg,type} lookup
  filters/      # mil/ladd/pia hex sets + PIA range check
  geo/          # haversine distance
  service/      # adsbv2.AdsbServiceServer
proto/adsb/v2/  # single proto file with google.api.http annotations
gen/adsb/v2/    # generated .pb.go, .pb.gw.go (committed)
third_party/    # vendored google/api/{annotations,http,httpbody}.proto
```

[dump1090]: https://github.com/flightaware/dump1090
[adsb.lol]: https://api.adsb.lol/
[grpc-gateway]: https://github.com/grpc-ecosystem/grpc-gateway
