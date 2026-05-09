// Package geo provides great-circle distance helpers used by the radius/closest
// endpoints.
package geo

import "math"

// EarthRadiusNM is the mean Earth radius in nautical miles.
const EarthRadiusNM = 3440.065

// DistanceNM returns the great-circle distance between two lat/lon points in
// nautical miles, using the haversine formula.
func DistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusNM * c
}
