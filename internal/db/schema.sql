CREATE TABLE IF NOT EXISTS vehicles (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    make            TEXT,
    model           TEXT,
    year            INTEGER NOT NULL,
    selling_price   INTEGER NOT NULL,
    km_driven       INTEGER NOT NULL,
    fuel            TEXT    NOT NULL,
    seller_type     TEXT,
    transmission    TEXT    NOT NULL,
    owner           TEXT,
    body_type       TEXT,
    seats           INTEGER,
    safety_rating   INTEGER,
    body_source     TEXT
);

CREATE INDEX IF NOT EXISTS idx_price ON vehicles(selling_price);
CREATE INDEX IF NOT EXISTS idx_km    ON vehicles(km_driven);
CREATE INDEX IF NOT EXISTS idx_fuel  ON vehicles(fuel);
CREATE INDEX IF NOT EXISTS idx_trans ON vehicles(transmission);
CREATE INDEX IF NOT EXISTS idx_body  ON vehicles(body_type);
