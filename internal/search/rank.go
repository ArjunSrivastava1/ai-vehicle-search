package search

import (
	"fmt"
	"sort"
)

// Weights are placeholders; tune after you see real queries.
const (
	wExact      = 0.4
	wPrice      = 0.3
	wSemantic   = 0.2 // 0 on day 1, wired on day 2
	wFreshness  = 0.1
)

func Rank(results []Vehicle, f Filter, query string) []Vehicle {
	for i := range results {
		v := &results[i]
		semScore := 0.0
		if query != "" {
			var err error
			semScore, err = SemanticScore(*v, query)
			if err != nil {
				// Log error but continue with neutral score
				fmt.Printf("warning: semantic score error: %v\n", err)
			}
		}
		v.Score = wExact*exactMatch(*v, f) +
			wPrice*priceProximity(*v, f) +
			wFreshness*freshness(*v) +
			wSemantic*semScore
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

func exactMatch(v Vehicle, f Filter) float64 {
	checks, hits := 0, 0
	if f.Fuel != "" { checks++; if v.Fuel == f.Fuel { hits++ } }
	if f.Transmission != "" { checks++; if v.Transmission == f.Transmission { hits++ } }
	if f.BodyType != "" { checks++; if v.BodyType == f.BodyType { hits++ } }
	if checks == 0 { return 1.0 }
	return float64(hits) / float64(checks)
}

func priceProximity(v Vehicle, f Filter) float64 {
	if f.PriceMax == nil || *f.PriceMax == 0 { return 0.5 }
	// closer to the budget cap scores higher, but anything within budget is fine
	ratio := float64(v.SellingPrice) / float64(*f.PriceMax)
	if ratio > 1 { return 0 }
	return ratio // 0..1
}

func freshness(v Vehicle) float64 {
	// crude: 2020 car → high, 2010 → low
	const minYear, maxYear = 2005, 2024
	if v.Year <= minYear { return 0 }
	if v.Year >= maxYear { return 1 }
	return float64(v.Year-minYear) / float64(maxYear-minYear)
}
