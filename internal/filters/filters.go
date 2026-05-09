// Package filters holds the hex-set predicates used by /v2/mil, /v2/ladd and
// /v2/pia. Exact matches are O(1); optional inclusive ranges are a linear
// scan but kept small in practice
package filters

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

type HexSet struct {
	exact  map[string]struct{}
	ranges []hexRange
}

type hexRange struct{ lo, hi uint32 }

func NewHexSet() *HexSet {
	return &HexSet{exact: map[string]struct{}{}}
}

func (s *HexSet) AddHex(h string) {
	h = strings.ToLower(strings.TrimSpace(h))
	if len(h) != 6 {
		return
	}
	s.exact[h] = struct{}{}
}

// AddRange inserts an inclusive [lo, hi] range. Order is normalised
func (s *HexSet) AddRange(lo, hi uint32) {
	if hi < lo {
		lo, hi = hi, lo
	}
	s.ranges = append(s.ranges, hexRange{lo, hi})
}

func (s *HexSet) Contains(hex string) bool {
	hex = strings.ToLower(hex)
	if _, ok := s.exact[hex]; ok {
		return true
	}
	if len(s.ranges) == 0 {
		return false
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return false
	}
	u := uint32(v)
	for _, r := range s.ranges {
		if u >= r.lo && u <= r.hi {
			return true
		}
	}
	return false
}

// LoadHexFile reads a `# comment`-aware text file with one hex (or `lo-hi`
// range) per line. Empty path returns an empty set
func LoadHexFile(path string) (*HexSet, error) {
	s := NewHexSet()
	if path == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseHexLines(s, f)
}

func parseHexLines(s *HexSet, r io.Reader) (*HexSet, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "-"); i > 0 {
			lo, errLo := strconv.ParseUint(strings.TrimSpace(line[:i]), 16, 32)
			hi, errHi := strconv.ParseUint(strings.TrimSpace(line[i+1:]), 16, 32)
			if errLo == nil && errHi == nil {
				s.AddRange(uint32(lo), uint32(hi))
				continue
			}
		}
		s.AddHex(line)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return s, nil
}

// IsPIA reports whether a hex falls in the FAA Privacy ICAO Address pool
// 0xADF000–0xADFFFF
func IsPIA(hex string) bool {
	hex = strings.ToLower(hex)
	if len(hex) != 6 {
		return false
	}
	return strings.HasPrefix(hex, "adf")
}

// DefaultMil seeds a HexSet with just the US military block. It's intentionally
// minimal — operators outside North America should supply -mil-file
func DefaultMil() *HexSet {
	s := NewHexSet()
	s.AddRange(0xAE0000, 0xAFFFFF)
	return s
}
