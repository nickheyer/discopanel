package main

import (
	"math"
	"sort"
	"time"
)

// Condenses world age observations into TPS stats
type tpsSummary struct {
	Median  float64
	P5      float64
	Min     float64
	Mean    float64
	Samples int
}

// Converts age observations into per-interval TPS values
func summarizeTPS(samples []tpsSample, rampSkip time.Duration) tpsSummary {
	if len(samples) < 2 {
		return tpsSummary{}
	}
	cutoff := samples[0].At.Add(rampSkip)
	var rates []float64
	for i := 1; i < len(samples); i++ {
		prev, cur := samples[i-1], samples[i]
		if cur.At.Before(cutoff) {
			continue
		}
		dt := cur.At.Sub(prev.At).Seconds()
		// Gaps mean the observer reconnected, not a stalled server
		if dt <= 0.2 || dt > 5 {
			continue
		}
		dAge := float64(cur.Age - prev.Age)
		if dAge < 0 {
			continue
		}
		rates = append(rates, dAge/dt)
	}
	if len(rates) == 0 {
		return tpsSummary{}
	}
	sorted := append([]float64(nil), rates...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, r := range sorted {
		sum += r
	}
	return tpsSummary{
		Median:  percentile(sorted, 50),
		P5:      percentile(sorted, 5),
		Min:     sorted[0],
		Mean:    sum / float64(len(sorted)),
		Samples: len(sorted),
	}
}

// Reads pth percentile from an ascending slice
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// Summarizes one metric across iterations as median, min, max
func medianOf(values []float64) (median, minV, maxV float64, ok bool) {
	var clean []float64
	for _, v := range values {
		if !math.IsNaN(v) {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return 0, 0, 0, false
	}
	sort.Float64s(clean)
	return percentile(clean, 50), clean[0], clean[len(clean)-1], true
}
