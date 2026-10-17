package search

import (
	"fmt"
	"sort"
	"sync"
)

// Weights are placeholders; tune after you see real queries.
const (
	wExact     = 0.35
	wPrice     = 0.25
	wSemantic  = 0.25 // Uses cosine similarity from embedding service
	wFreshness = 0.15
)

// semanticConcurrency caps how many in-flight requests we send to the
// embedding service. Too low → wasted wall time; too high → queueing on the
// Python side and no benefit. 8 is a reasonable default for a local service.
const semanticConcurrency = 8

// Rank scores and orders the candidate vehicles.
//
// Semantic scores are fetched concurrently from the embedding service, with a
// bounded worker pool. Everything else is pure CPU and runs inline.
func Rank(results []Vehicle, f Filter, query string) []Vehicle {
	if query != "" && len(results) > 0 {
		// Fetch semantic scores concurrently.
		semScores := make([]float64, len(results))

		var wg sync.WaitGroup
		sem := make(chan struct{}, semanticConcurrency)

		for i := range results {
			wg.Add(1)
			sem <- struct{}{} // acquire a slot
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }() // release the slot

				score, err := SemanticScore(results[i], query)
				if err != nil {
					fmt.Printf("warning: semantic score error for id=%d: %v\n", results[i].ID, err)
					score = 0.5
				}
				semScores[i] = score
			}(i)
		}

		wg.Wait()

		// Apply the semantic term after all goroutines have joined, so
		// there's no write contention on the slice.
		for i := range results {
			v := &results[i]
			v.Score = wExact*exactMatch(*v, f) +
				wPrice*priceProximity(*v, f) +
				wFreshness*yearMatch(*v, f) +
				wSemantic*semScores[i]
		}
	} else {
		// No query: score with structured terms only.
		for i := range results {
			v := &results[i]
			v.Score = wExact*exactMatch(*v, f) +
				wPrice*priceProximity(*v, f) +
				wFreshness*yearMatch(*v, f)
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

func exactMatch(v Vehicle, f Filter) float64 {
	checks, hits := 0, 0
	if f.Fuel != "" {
		checks++
		if v.Fuel == f.Fuel {
			hits++
		}
	}
	if f.Transmission != "" {
		checks++
		if v.Transmission == f.Transmission {
			hits++
		}
	}
	if f.BodyType != "" {
		checks++
		if v.BodyType == f.BodyType {
			hits++
		}
	}
	if f.SeatsMin != nil && *f.SeatsMin > 0 {
		checks++
		if v.Seats >= *f.SeatsMin {
			hits++
		}
	}
	if f.SafetyMin != nil && *f.SafetyMin > 0 {
		checks++
		if v.SafetyRating >= *f.SafetyMin {
			hits++
		}
	}
	if checks == 0 {
		return 1.0
	}
	return float64(hits) / float64(checks)
}

func priceProximity(v Vehicle, f Filter) float64 {
	if f.PriceMax == nil || *f.PriceMax == 0 {
		return 0.5
	}
	// closer to the budget cap scores higher, but anything within budget is fine
	ratio := float64(v.SellingPrice) / float64(*f.PriceMax)
	if ratio > 1 {
		return 0
	}
	return ratio // 0..1
}

func yearMatch(v Vehicle, f Filter) float64 {
	const minYear, maxYear = 2005, 2024

	// No constraint → full credit
	if (f.YearMin == nil || *f.YearMin == 0) &&
		(f.YearMax == nil || *f.YearMax == 0) {
		return 1.0
	}

	score := 1.0

	// If car is older than the minimum, decay based on distance from minYear
	if f.YearMin != nil && *f.YearMin > 0 && v.Year < *f.YearMin {
		span := float64(*f.YearMin - minYear)
		if span <= 0 {
			score = 0
		} else {
			score = float64(v.Year-minYear) / span
		}
	}

	// If car is newer than the maximum, decay based on distance from maxYear
	if f.YearMax != nil && *f.YearMax > 0 && v.Year > *f.YearMax {
		span := float64(maxYear - *f.YearMax)
		part := 0.0
		if span > 0 {
			part = float64(maxYear-v.Year) / span
		}
		if part < score {
			score = part
		}
	}

	// Clamp
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
