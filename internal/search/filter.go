package search

import (
	"database/sql"
)

type Filter struct {
	Fuel         string `json:"fuel,omitempty"`
	Transmission string `json:"transmission,omitempty"`
	BodyType     string `json:"body_type,omitempty"`
	PriceMin     *int   `json:"price_min,omitempty"`
	PriceMax     *int   `json:"price_max,omitempty"`
	KMMax        *int   `json:"km_max,omitempty"`
	SeatsMin     *int   `json:"seats_min,omitempty"`
	SeatsMax     *int   `json:"seats_max,omitempty"`
	SafetyMin    *int   `json:"safety_rating_min,omitempty"`
	SafetyMax    *int   `json:"safety_rating_max,omitempty"`
	YearMin      *int   `json:"year_min,omitempty"`
	YearMax      *int   `json:"year_max,omitempty"`
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type Vehicle struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Year         int     `json:"year"`
	SellingPrice int     `json:"selling_price"`
	KMDriven     int     `json:"km_driven"`
	Fuel         string  `json:"fuel"`
	Transmission string  `json:"transmission"`
	SellerType   string  `json:"seller_type,omitempty"`
	Owner        string  `json:"owner,omitempty"`
	BodyType     string  `json:"body_type,omitempty"`
	Seats        int     `json:"seats,omitempty"`
	SafetyRating int     `json:"safety_rating,omitempty"`
	Score        float64 `json:"score"`
}

// fetch queries the database with the given filter and returns matching vehicles
func fetch1(d *sql.DB, f Filter) ([]Vehicle, error) {
	query := `SELECT id, name, year, selling_price, km_driven, fuel, 
	             transmission, COALESCE(seller_type, ''), COALESCE(owner, ''),
	             COALESCE(body_type, ''), COALESCE(seats, 0), COALESCE(safety_rating, 0)
	            FROM vehicles
	            WHERE body_source != 'unresolved'`

	args := []interface{}{}

	// Build WHERE conditions
	if f.Fuel != "" {
		query += " AND fuel = ?"
		args = append(args, f.Fuel)
	}
	if f.Transmission != "" {
		query += " AND transmission = ?"
		args = append(args, f.Transmission)
	}
	if f.BodyType != "" {
		query += " AND body_type = ?"
		args = append(args, f.BodyType)
	}
	if f.PriceMin != nil && *f.PriceMin > 0 {
		query += " AND selling_price >= ?"
		args = append(args, *f.PriceMin)
	}
	if f.PriceMax != nil && *f.PriceMax > 0 {
		query += " AND selling_price <= ?"
		args = append(args, *f.PriceMax)
	}
	if f.KMMax != nil && *f.KMMax > 0 {
		query += " AND km_driven <= ?"
		args = append(args, *f.KMMax)
	}
	if f.SeatsMin != nil && *f.SeatsMin > 0 {
		query += " AND seats >= ?"
		args = append(args, *f.SeatsMin)
	}
	if f.SeatsMax != nil && *f.SeatsMax > 0 {
		query += " AND seats <= ?"
		args = append(args, *f.SeatsMax)
	}
	if f.SafetyMin != nil && *f.SafetyMin > 0 {
		query += " AND safety_rating >= ?"
		args = append(args, *f.SafetyMin)
	}
	if f.SafetyMax != nil && *f.SafetyMax > 0 {
		query += " AND safety_rating <= ?"
		args = append(args, *f.SafetyMax)
	}
	if f.YearMin != nil && *f.YearMin > 0 {
		query += " AND year >= ?"
		args = append(args, *f.YearMin)
	}
	if f.YearMax != nil && *f.YearMax > 0 {
		query += " AND year <= ?"
		args = append(args, *f.YearMax)
	}

	// Add pagination
	if f.Limit <= 0 || f.Limit > 50 {
		f.Limit = 10
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	query += " ORDER BY year DESC, selling_price ASC LIMIT ? OFFSET ?"
	args = append(args, f.Limit, f.Offset)

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Vehicle
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.Name, &v.Year, &v.SellingPrice, &v.KMDriven,
			&v.Fuel, &v.Transmission, &v.SellerType, &v.Owner,
			&v.BodyType, &v.Seats, &v.SafetyRating); err != nil {
			return nil, err
		}
		results = append(results, v)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
