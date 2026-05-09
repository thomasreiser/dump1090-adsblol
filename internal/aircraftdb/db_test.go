package aircraftdb

import (
	"strings"
	"testing"
)

const sample = `# header comment
a1b2c3,N123AA,A320
A1B2C4,N124AA,A320
def456,C-FXYZ,B738

# blank line above
bad,,
short,X
`

func TestParseCSV(t *testing.T) {
	db, err := parseCSV(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if e := db.Lookup("a1b2c3"); e.Registration != "N123AA" || e.IcaoType != "A320" {
		t.Errorf("a1b2c3 = %+v", e)
	}
	if e := db.Lookup("A1B2C4"); e.Hex != "a1b2c4" || e.Registration != "N124AA" {
		t.Errorf("A1B2C4 lookup not case-folded: %+v", e)
	}
	if hx := db.HexesByType("a320"); len(hx) != 2 {
		t.Errorf("type=A320 hexes = %v, want 2", hx)
	}
	if hx := db.HexesByRegistration("c-fxyz"); len(hx) != 1 || hx[0] != "def456" {
		t.Errorf("reg=C-FXYZ hexes = %v", hx)
	}
	if e := db.Lookup("zzzzzz"); e.Hex != "" {
		t.Errorf("unknown hex returned %+v", e)
	}
}

func TestEmpty(t *testing.T) {
	d := Empty()
	if e := d.Lookup("a1b2c3"); e.Hex != "" {
		t.Errorf("empty lookup = %+v", e)
	}
}
