package geo

import (
	"math"
	"testing"
)

func TestDistanceNM_Identity(t *testing.T) {
	if d := DistanceNM(40, -74, 40, -74); d != 0 {
		t.Errorf("identity = %v, want 0", d)
	}
}

func TestDistanceNM_KnownPair(t *testing.T) {
	// JFK to LAX is ~2144 NM
	d := DistanceNM(40.6413, -73.7781, 33.9425, -118.4081)
	if math.Abs(d-2144) > 20 {
		t.Errorf("JFK-LAX = %v NM, want ~2144", d)
	}
}
