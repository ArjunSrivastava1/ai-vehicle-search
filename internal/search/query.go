package search

import (
	"database/sql"
	"strings"
)

func fetch(d *sql.DB, f Filter) ([]Vehicle, error) {
	var (
		where []string
		args  []any
	)

	if f.Fuel != "" {
		where = append(where, "fuel = ? COLLATE NOCASE")
		args = append(args, f.Fuel)
	}

	if f.Transmission != "" {
		// AMT is an automated manual — treat it as "automatic" for user queries,
		// since that's what "automatic" means in practice on the Indian market.
		if strings.EqualFold(f.Transmission, "automatic") {
			where = append(where, "(transmission = ? COLLATE NOCASE OR transmission = ? COLLATE NOCASE)")
			args = append(args, "automatic", "amt")
		} else {
			where = append(where, "transmission = ? COLLATE NOCASE")
			args = append(args, f.Transmission)
		}
	}

	if f.BodyType != "" {
		where = append(where, "body_type = ? COLLATE NOCASE")
		args = append(args, f.BodyType)
	}

	if f.YearMin != nil {
		where = append(where, "year >= ?")
		args = append(args, *f.YearMin)
	}

	if f.YearMax != nil {
		where = append(where, "year <= ?")
		args = append(args, *f.YearMax)
	}

	if f.PriceMin != nil {
		where = append(where, "selling_price >= ?")
		args = append(args, *f.PriceMin)
	}
	if f.PriceMax != nil {
		where = append(where, "selling_price <= ?")
		args = append(args, *f.PriceMax)
	}
	if f.KMMax != nil {
		where = append(where, "km_driven <= ?")
		args = append(args, *f.KMMax)
	}
	if f.SeatsMin != nil {
		where = append(where, "seats >= ?")
		args = append(args, *f.SeatsMin)
	}
	if f.SafetyMin != nil {
		where = append(where, "safety_rating >= ?")
		args = append(args, *f.SafetyMin)
	}

	q := "SELECT MIN(id), name, year, selling_price, km_driven, fuel, transmission, " +
		"COALESCE(seller_type,''), COALESCE(owner,''), COALESCE(body_type,''), " +
		"COALESCE(seats,0), COALESCE(safety_rating,0) FROM vehicles"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " GROUP BY name, year, selling_price, km_driven, fuel, transmission, " +
		"COALESCE(seller_type,''), COALESCE(owner,''), COALESCE(body_type,''), " +
		"COALESCE(seats,0), COALESCE(safety_rating,0)"
	q += " ORDER BY year DESC, selling_price ASC LIMIT 100"

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Initialize as empty slice so JSON serialization gives [] not null.
	out := []Vehicle{}
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.Name, &v.Year, &v.SellingPrice, &v.KMDriven,
			&v.Fuel, &v.Transmission, &v.SellerType, &v.Owner,
			&v.BodyType, &v.Seats, &v.SafetyRating); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
