package search

import (
	"database/sql"
	"fmt"
	"strings"
)

func fetch(d *sql.DB, f Filter) ([]Vehicle, error) {
	var (
		where []string
		args  []any
	)
	if f.Fuel != "" {
		where = append(where, "fuel = ?")
		args = append(args, f.Fuel)
	}
	if f.Transmission != "" {
		where = append(where, "transmission = ?")
		args = append(args, f.Transmission)
	}
	if f.BodyType != "" {
		where = append(where, "body_type = ?")
		args = append(args, f.BodyType)
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

	q := "SELECT id, name, year, selling_price, km_driven, fuel, transmission, " +
		"COALESCE(seller_type,''), COALESCE(owner,''), COALESCE(body_type,''), " +
		"COALESCE(seats,0), COALESCE(safety_rating,0) FROM vehicles"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " LIMIT 500" // hard cap before ranking

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Vehicle
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.Name, &v.Year, &v.SellingPrice, &v.KMDriven,
			&v.Fuel, &v.Transmission, &v.SellerType, &v.Owner,
			&v.BodyType, &v.Seats, &v.SafetyRating); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return out, nil
	}
	_ = fmt.Sprint // placeholder if you extend
	return out, nil
}
