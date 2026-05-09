package filters

import (
	"strings"
	"testing"
)

func TestHexSet_ExactAndRange(t *testing.T) {
	s := NewHexSet()
	s.AddHex("A1B2C3")
	s.AddRange(0xAE0000, 0xAFFFFF)

	cases := []struct {
		hex  string
		want bool
	}{
		{"a1b2c3", true},
		{"A1B2C3", true},
		{"ae0000", true},
		{"afffff", true},
		{"af1234", true},
		{"b00000", false},
		{"123456", false},
		{"toolong", false},
	}
	for _, c := range cases {
		if got := s.Contains(c.hex); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.hex, got, c.want)
		}
	}
}

func TestParseHexLines(t *testing.T) {
	const data = `# comment line
a1b2c3
ae0000-afffff
abcdef

# another
not-hex
`
	s, err := parseHexLines(NewHexSet(), strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Contains("a1b2c3") {
		t.Error("missing a1b2c3")
	}
	if !s.Contains("abcdef") {
		t.Error("missing abcdef")
	}
	if !s.Contains("ae1234") {
		t.Error("range ae0000-afffff should include ae1234")
	}
	if s.Contains("b00000") {
		t.Error("range ae0000-afffff should not include b00000")
	}
}

func TestIsPIA(t *testing.T) {
	cases := map[string]bool{
		"adf000": true,
		"adffff": true,
		"adf123": true,
		"ade000": false,
		"abc123": false,
		"":       false,
		"ad":     false,
	}
	for h, want := range cases {
		if got := IsPIA(h); got != want {
			t.Errorf("IsPIA(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestDefaultMil(t *testing.T) {
	s := DefaultMil()
	if !s.Contains("ae0000") || !s.Contains("afffff") {
		t.Error("DefaultMil should cover US mil block")
	}
	if s.Contains("ad0000") {
		t.Error("DefaultMil should not include 0xAD0000")
	}
}
