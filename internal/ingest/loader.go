package ingest

import (
	"database/sql"
	"encoding/csv"
	"os"
	"strconv"
	"strings"
)

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
	r.FieldsPerRecord = -1 // tolerate ragged rows
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

		var bodyType, source sql.NullString
		var seats, safety sql.NullInt64
		if d, ok := lookup.Derive(name); ok {
			bodyType = sql.NullString{String: d.BodyType, Valid: true}
			seats = sql.NullInt64{Int64: int64(d.Seats), Valid: true}
			safety = sql.NullInt64{Int64: int64(d.SafetyRating), Valid: true}
			source = sql.NullString{String: "lookup", Valid: true}
		} else {
			source = sql.NullString{String: "unresolved", Valid: true}
			unresolved++
		}

		if _, err := stmt.Exec(
			name, year, price, km,
			get(row, header, "fuel"),
			get(row, header, "seller_type"),
			get(row, header, "transmission"),
			get(row, header, "owner"),
			bodyType, seats, safety, source,
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
