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

func LoadLookup(path string) (Lookup, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Lookup
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return l, nil
}

// Derive returns the derived fields for a car name, or zero values + false
// if the model token isn't in the lookup.
func (l Lookup) Derive(name string) (Derived, bool) {
	lower := strings.ToLower(name)
	// Match the longest key that appears in the name — "xuv700" beats "xuv".
	var best string
	for k := range l {
		if strings.Contains(lower, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return Derived{}, false
	}
	return l[best], true
}
