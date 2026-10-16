package db

import (
	"database/sql"
	_ "embed"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

func Open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(schemaSQL); err != nil {
		return nil, err
	}
	return d, nil
}
