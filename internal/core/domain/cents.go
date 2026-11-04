package domain

import "math"

// RoundCents is usd rounded to whole cents, the amount a person reads: a float sum or difference
// of run costs carries binary rounding error (0.1+0.2 is 0.30000000000000004).
func RoundCents(usd float64) float64 { return math.Round(usd*100) / 100 }
