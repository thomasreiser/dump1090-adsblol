// Package aircraftdb loads optional hex→{registration,type} metadata used to
// populate the `r` and `t` fields on responses, and to back the /v2/reg and
// /v2/icao endpoints.
//
// The expected CSV layout is `hex,registration,icaotype` with no header, e.g.:
//
//	a1b2c3,N123AA,A320
//	c01a2b,C-FXYZ,B738
//
// Lines starting with `#` and blank lines are ignored. The hex column is
// canonicalized to lowercase; registration and type are kept as-given.
package aircraftdb

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// Entry is one row.
type Entry struct {
	Hex          string
	Registration string
	IcaoType     string
}

// DB is an immutable in-memory index built at startup.
type DB struct {
	byHex map[string]Entry
	byReg map[string][]Entry // upper-cased registration → entries
	byTyp map[string][]Entry // upper-cased icao type → entries
}

// Empty returns a DB with no entries; lookups all return zero values.
func Empty() *DB {
	return &DB{
		byHex: map[string]Entry{},
		byReg: map[string][]Entry{},
		byTyp: map[string][]Entry{},
	}
}

// LoadCSV loads a database from the file at path. An empty path returns an
// empty DB so callers can unconditionally pass it through.
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
	lineNum := 0
	for sc.Scan() {
		lineNum++
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

// Lookup returns the entry for a hex or zero value.
func (d *DB) Lookup(hex string) Entry {
	return d.byHex[strings.ToLower(hex)]
}

// HexesByRegistration returns all hexes with the given registration (upper-cased match).
func (d *DB) HexesByRegistration(reg string) []string {
	return hexes(d.byReg[strings.ToUpper(strings.TrimSpace(reg))])
}

// HexesByType returns all hexes with the given ICAO type designator.
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
