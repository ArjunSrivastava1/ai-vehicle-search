package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"cars24-search/internal/db"
	"cars24-search/internal/ingest"
	"cars24-search/internal/search"
)

func main() {
	d, err := db.Open("./cars.db")
	if err != nil {
		log.Fatal(err)
	}

	// Ingest once — comment out after first run, or guard on a count check.
	var count int
	d.QueryRow("SELECT COUNT(*) FROM vehicles").Scan(&count)
	if count == 0 {
		loaded, unresolved, err := ingest.LoadCSV(d, "./data/vehicles.csv", "./data/derived_lookup.json")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("ingested %d rows, %d unresolved", loaded, unresolved)
	} else {
		log.Printf("db already populated: %d rows", count)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /search/filter", func(w http.ResponseWriter, r *http.Request) {
		var f search.Filter
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		results, err := search.Run(d, f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"filters": f,
			"count":   len(results),
			"results": results,
		})
	})

	mux.HandleFunc("POST /search/semantic", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Query == "" {
			http.Error(w, "query is required", http.StatusBadRequest)
			return
		}
		results, err := search.RunSemantic(d, search.Filter{}, req.Query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"query": req.Query,
			"count": len(results),
			"results": results,
		})
	})
	mux.HandleFunc("GET /vehicles/{id}", func(w http.ResponseWriter, r *http.Request) {
		var v search.Vehicle
		err := d.QueryRow(
			`SELECT id,name,year,selling_price,km_driven,fuel,transmission,
			        COALESCE(seller_type,''),COALESCE(owner,''),
			        COALESCE(body_type,''),COALESCE(seats,0),COALESCE(safety_rating,0)
			 FROM vehicles WHERE id = ?`, r.PathValue("id")).
			Scan(&v.ID, &v.Name, &v.Year, &v.SellingPrice, &v.KMDriven,
				&v.Fuel, &v.Transmission, &v.SellerType, &v.Owner,
				&v.BodyType, &v.Seats, &v.SafetyRating)
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	})

	log.Println("listening on :3000")
	log.Fatal(http.ListenAndServe(":3000", mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
