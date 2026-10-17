package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"cars24-search/internal/db"
	"cars24-search/internal/ingest"
	"cars24-search/internal/search"
)

// CheckEmbeddingServiceHealth checks the health of the embedding service
func CheckEmbeddingServiceHealth() (bool, string) {
	resp, err := http.Get(search.GetServiceURL() + "/health")
	if err != nil {
		return false, "embedding service not reachable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "embedding service returned error status"
	}

	var healthResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return false, "failed to decode health response"
	}

	healthy := healthResp["healthy"] == true
	errorMsg, _ := healthResp["error"].(string)

	return healthy, errorMsg
}

func main() {
	d, err := db.Open("./cars.db")
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	// Ingest once — comment out after first run, or guard on a count check.
	var count int
	d.QueryRow("SELECT COUNT(*) FROM vehicles").Scan(&count)
	if count == 0 {
		loaded, unresolved, err := ingest.LoadCSV(d, "./data/car_details_v3.csv", "./data/derived_lookup.json")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("ingested %d rows, %d unresolved", loaded, unresolved)
	} else {
		log.Printf("db already populated: %d rows", count)
	}

	// Start embedding service as a subprocess
	ctx := context.Background()
	pythonBin := os.Getenv("PYTHON_BIN")
	if pythonBin == "" {
		pythonBin = "./service.venv/bin/python" // fallback: assume venv in CWD
	}
	cmd := exec.CommandContext(ctx, pythonBin, "./embedding_service.py")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start embedding service: %v", err)
	}

	log.Println("Starting embedding service...")

	// Wait for embedding service to be healthy
	pollInterval := 2 * time.Second
	maxPoll := 60
	healthy := false
	serviceURL := search.GetServiceURL()

	// IMPORTANT: declare errMsg OUTSIDE the loop so we don't shadow `healthy`.
	var errMsg string
	for i := 0; i < maxPoll && !healthy; i++ {
		time.Sleep(pollInterval)

		// NOTE: `=` not `:=` — assign to the outer `healthy`, don't shadow it.
		healthy, errMsg = CheckEmbeddingServiceHealth()
		if healthy {
			log.Printf("Embedding service is healthy at %s", serviceURL)
			break
		}
		log.Printf("Embedding service not ready yet (%d/%d): %s", i+1, maxPoll, errMsg)
	}

	if !healthy {
		log.Printf("Embedding service at %s did not become healthy within %s; semantic scoring will fall back to neutral.",
			serviceURL, time.Duration(maxPoll)*pollInterval)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		healthy, errorMsg := CheckEmbeddingServiceHealth()
		data := map[string]any{
			"status":     "healthy",
			"embeddings": healthy,
			"service":    "cars24-search",
			"version":    "1.0.0",
			"db_rows":    count,
		}

		if !healthy {
			data["error"] = errorMsg
			data["status"] = "unhealthy"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(data)
			return
		}

		writeJSON(w, data)
	})

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

		// NEW: extract structured constraints from the query text
		// so that "under 10 lakh", "automatic", "SUV" etc. actually filter.
		f := search.ParseQuery(req.Query)

		results, err := search.RunSemantic(d, f, req.Query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"query":         req.Query,
			"parsed_filter": f,
			"count":         len(results),
			"results":       results,
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

	// Graceful shutdown: kill the child, then let ListenAndServe return.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait() // reap the child so port 3001 is released
		}
		os.Exit(0)
	}()

	log.Println("listening on :3000")
	log.Fatal(http.ListenAndServe(":3000", mux))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
