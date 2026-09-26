package alert

import "math"

const earthRadiusM = 6_371_000.0

// DistanceM is the great-circle distance in metres, which at city scale is
// accurate to well under a metre.
func DistanceM(lat1, lng1, lat2, lng2 float64) float64 {
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	phi1, phi2 := rad(lat1), rad(lat2)
	dPhi, dLambda := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Pow(math.Sin(dPhi/2), 2) + math.Cos(phi1)*math.Cos(phi2)*math.Pow(math.Sin(dLambda/2), 2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(math.Min(1, a)))
}
