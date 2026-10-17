package ingest

import (
	"encoding/json"
	"os"
	"strings"
)

type Derived struct {
	BodyType     string `json:"body_type"`
	Seats        int    `json:"seats"`
	SafetyRating int    `json:"safety_rating"`
}

type Lookup map[string]Derived

// normalizeName lowercases and strips spaces, hyphens, and underscores.
// Both the CSV vehicle names and the lookup keys are run through this so
// that "Maruti Wagon R LXI" matches the lookup key "wagon-r".
func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)
	return s
}

func LoadLookup(path string) (Lookup, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw Lookup
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	// Normalize keys once at load time so Derive doesn't need to.
	normalized := make(Lookup, len(raw))
	for k, v := range raw {
		normalized[normalizeName(k)] = v
	}
	return normalized, nil
}

// Derive returns the derived fields for a car name, or zero values + false
// if no lookup key is contained in the name. Longest match wins, so "xuv700"
// beats "xuv" for a name like "Mahindra XUV700 AXE".
func (l Lookup) Derive(name string) (Derived, bool) {
	normalized := normalizeName(name)

	var best string
	var bestVal Derived
	for k, v := range l {
		// k is already normalized by LoadLookup.
		if strings.Contains(normalized, k) && len(k) > len(best) {
			best = k
			bestVal = v
		}
	}
	if best == "" {
		return Derived{}, false
	}
	return bestVal, true
}
