package ingest

import (
	"database/sql"
	"encoding/csv"
	"os"
	"strconv"
	"strings"
)

// normalize lowercases and trims a categorical string so query-time
// comparisons (COLLATE NOCASE) always match.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// parseSeats handles the CSV seats column, which might be empty or "5.0".
func parseSeats(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f), true
	}
	return 0, false
}

func LoadCSV(d *sql.DB, csvPath, lookupPath string) (loaded, unresolved int, err error) {
	lookup, err := LoadLookup(lookupPath)
	if err != nil {
		return 0, 0, err
	}

	f, err := os.Open(csvPath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return 0, 0, err
	}
	if len(rows) < 2 {
		return 0, 0, nil
	}

	header := indexHeader(rows[0])
	tx, err := d.Begin()
	if err != nil {
		return 0, 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO vehicles
		(name, year, selling_price, km_driven, fuel, seller_type, transmission, owner,
		 body_type, seats, safety_rating, body_source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()

	for _, row := range rows[1:] {
		name := get(row, header, "name")
		if name == "" {
			continue
		}
		year, _ := strconv.Atoi(get(row, header, "year"))
		price, _ := strconv.Atoi(get(row, header, "selling_price"))
		km, _ := strconv.Atoi(get(row, header, "km_driven"))

		fuel := normalize(get(row, header, "fuel"))
		trans := normalize(get(row, header, "transmission"))
		seller := get(row, header, "seller_type")
		owner := normalize(get(row, header, "owner"))

		var bodyType string
		var seats, safety int
		haveBody, haveSeats, haveSafety := false, false, false

		// 1. Try CSV seats
		if s, ok := parseSeats(get(row, header, "seats")); ok && s > 0 {
			seats = s
			haveSeats = true
		}

		// 2. Try lookup for body_type, safety, and (if CSV didn't have) seats
		src := "unresolved"
		if derived, ok := lookup.Derive(name); ok {
			bodyType = normalize(derived.BodyType)
			haveBody = true
			if !haveSeats && derived.Seats > 0 {
				seats = derived.Seats
				haveSeats = true
			}
			if derived.SafetyRating > 0 {
				safety = derived.SafetyRating
				haveSafety = true
			}
			if haveSeats && haveBody && haveSafety {
				src = "csv+lookup"
			} else {
				src = "lookup"
			}
		} else if haveSeats {
			src = "csv"
		}

		if src == "unresolved" {
			unresolved++
		}

		var bodyVal, sourceVal sql.NullString
		var seatsVal, safetyVal sql.NullInt64
		if haveBody {
			bodyVal = sql.NullString{String: bodyType, Valid: true}
		}
		if haveSeats {
			seatsVal = sql.NullInt64{Int64: int64(seats), Valid: true}
		}
		if haveSafety {
			safetyVal = sql.NullInt64{Int64: int64(safety), Valid: true}
		}
		sourceVal = sql.NullString{String: src, Valid: true}

		if _, err := stmt.Exec(
			name, year, price, km,
			fuel, seller, trans, owner,
			bodyVal, seatsVal, safetyVal, sourceVal,
		); err != nil {
			return 0, 0, err
		}
		loaded++
	}
	return loaded, unresolved, tx.Commit()
}

func indexHeader(h []string) map[string]int {
	m := make(map[string]int, len(h))
	for i, name := range h {
		m[strings.ToLower(strings.TrimSpace(name))] = i
	}
	return m
}

func get(row []string, h map[string]int, key string) string {
	i, ok := h[key]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
