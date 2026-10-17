package main

// cmd/coverage/main.go

import (
	"encoding/csv"
	"fmt"
	"os"

	"cars24-search/internal/ingest"
)

func main() {
	lookup, err := ingest.LoadLookup("./data/derived_lookup.json")
	if err != nil {
		panic(err)
	}
	f, _ := os.Open("./data/car details v3.csv")
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, _ := r.ReadAll()

	hits, misses, total := 0, 0, 0
	missNames := make(map[string]bool)
	missRows := 0
	for _, row := range rows[1:] {
		if len(row) == 0 || row[0] == "" {
			continue
		}
		total++
		if _, ok := lookup.Derive(row[0]); ok {
			hits++
		} else {
			misses++
			missRows++
			missNames[row[0]] = true
		}
	}
	fmt.Printf("rows: total=%d hits=%d misses=%d coverage=%.1f%%\n",
		total, hits, misses, 100*float64(hits)/float64(total))
	fmt.Printf("unique names missed: %d\n", len(missNames))

	// print top 30 most common missed names so we know what to add to lookup
	type nameCount struct {
		name  string
		count int
	}
	counts := make(map[string]int)
	for _, row := range rows[1:] {
		if len(row) == 0 || row[0] == "" {
			continue
		}
		if _, ok := lookup.Derive(row[0]); !ok {
			counts[row[0]]++
		}
	}
	var top []nameCount
	for n, c := range counts {
		top = append(top, nameCount{n, c})
	}
	for i := 0; i < len(top); i++ {
		for j := i + 1; j < len(top); j++ {
			if top[j].count > top[i].count {
				top[i], top[j] = top[j], top[i]
			}
		}
	}
	fmt.Println("\ntop 30 most common missed names (with row counts):")
	for i := 0; i < 30 && i < len(top); i++ {
		fmt.Printf("  %5d  %s\n", top[i].count, top[i].name)
	}
}
