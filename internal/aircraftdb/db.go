// Package aircraftdb loads an optional hex→{registration,type} CSV used to
// fill the `r`/`t` response fields and to back /v2/reg and /v2/icao
//
// CSV layout (no header): hex,registration,icaotype. # comments and blank
// lines are skipped, hex is folded to lowercase
package aircraftdb

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

type Entry struct {
	Hex          string
	Registration string
	IcaoType     string
}

type DB struct {
	byHex map[string]Entry
	byReg map[string][]Entry // key is upper-cased registration
	byTyp map[string][]Entry // key is upper-cased ICAO type
}

func Empty() *DB {
	return &DB{
		byHex: map[string]Entry{},
		byReg: map[string][]Entry{},
		byTyp: map[string][]Entry{},
	}
}

// LoadCSV reads the file at path. An empty path returns an empty DB so callers
// don't need to special-case the "no DB configured" path
func LoadCSV(path string) (*DB, error) {
	if path == "" {
		return Empty(), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseCSV(f)
}

func parseCSV(r io.Reader) (*DB, error) {
	db := Empty()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		hex := strings.ToLower(strings.TrimSpace(parts[0]))
		if len(hex) != 6 {
			continue
		}
		e := Entry{
			Hex:          hex,
			Registration: strings.TrimSpace(parts[1]),
		}
		if len(parts) >= 3 {
			e.IcaoType = strings.TrimSpace(parts[2])
		}
		db.byHex[hex] = e
		if e.Registration != "" {
			k := strings.ToUpper(e.Registration)
			db.byReg[k] = append(db.byReg[k], e)
		}
		if e.IcaoType != "" {
			k := strings.ToUpper(e.IcaoType)
			db.byTyp[k] = append(db.byTyp[k], e)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return db, nil
}

func (d *DB) Lookup(hex string) Entry {
	return d.byHex[strings.ToLower(hex)]
}

func (d *DB) HexesByRegistration(reg string) []string {
	return hexes(d.byReg[strings.ToUpper(strings.TrimSpace(reg))])
}

func (d *DB) HexesByType(typ string) []string {
	return hexes(d.byTyp[strings.ToUpper(strings.TrimSpace(typ))])
}

func hexes(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Hex
	}
	return out
}
