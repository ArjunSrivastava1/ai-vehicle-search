package search

import (
	"database/sql"
)

func Run(d *sql.DB, f Filter) ([]Vehicle, error) {
	if f.Limit <= 0 || f.Limit > 50 {
		f.Limit = 10
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := fetch(d, f)
	if err != nil {
		return nil, err
	}
	rows = Rank(rows, f, "")
	if len(rows) > f.Limit {
		rows = rows[:f.Limit]
	}
	return rows, nil
}

// RunSemantic runs search with semantic scoring enabled for the given query.
func RunSemantic(d *sql.DB, f Filter, query string) ([]Vehicle, error) {
	if f.Limit <= 0 || f.Limit > 50 {
		f.Limit = 10
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := fetch(d, f)
	if err != nil {
		return nil, err
	}
	rows = Rank(rows, f, query)
	if len(rows) > f.Limit {
		rows = rows[:f.Limit]
	}
	return rows, nil
}
