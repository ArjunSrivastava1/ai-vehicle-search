# cars24-search

Natural-language vehicle search backend. Ask for *"automatic 7 seater SUV under 20 lakh from 2018"* and get back cars that satisfy **all five constraints**, ranked by semantic similarity to the query.

Built for the Cars24 backend engineering assignment, Problem 2.

---

## What it does

Users type free-text queries. The service:

1. **Extracts structured constraints** from the text — body type, price ceiling, transmission, fuel, seats, year range, safety rating.
2. **Filters the SQLite catalogue** with those constraints as hard `WHERE` clauses.
3. **Scores and ranks** the surviving candidates using a hybrid of structured match, price proximity, year freshness, and **cosine similarity** to the query embedding.
4. Returns a filtered, ordered result set with a `parsed_filter` field so the client can show what was understood.

---

## Demo

```bash
curl -s -X POST http://localhost:3000/search/semantic \
  -H 'Content-Type: application/json' \
  -d '{"query":"automatic 7 seater SUV under 20 lakh from 2018"}' | jq
```

```json
{
  "query": "automatic 7 seater SUV under 20 lakh from 2018",
  "parsed_filter": {
    "transmission": "automatic",
    "body_type": "suv",
    "price_max": 2000000,
    "seats_min": 7,
    "year_min": 2018
  },
  "count": 2,
  "results": [
    {
      "name": "Mahindra XUV500 W11 AT BSIV",
      "year": 2019,
      "transmission": "Automatic",
      "seats": 7,
      "selling_price": 1650000,
      "score": 0.907
    },
    {
      "name": "Mahindra XUV500 AT W9 2WD",
      "year": 2018,
      "transmission": "Automatic",
      "seats": 7,
      "selling_price": 1500000,
      "score": 0.889
    }
  ]
}
```

Five constraints extracted from one sentence. Two cars actually match all of them. Every returned car satisfies every constraint.

Other example queries that work:

| Query | Extracts |
| --- | --- |
| SUV under 10 lakh | body_type, price_max |
| diesel hatchback under 5 lakh | fuel, body_type, price_max |
| 5 star safety SUV | body_type, safety_rating_min |
| SUV from 2020 to 2023 | body_type, year_min, year_max |
| petrol manual hatchback under 4 lakh | fuel, transmission, body_type, price_max |

## Architecture

```
┌──────────────┐    POST /search/semantic
│   Client     │ ─────────────────────────────┐
└──────────────┘                              ▼
                                     ┌────────────────────────────┐
                                     │   Go HTTP server (:3000)   │
                                     │                            │
                                     │  ParseQuery  text→Filter   │
                                     │      │                     │
                                     │      ▼                     │
                                     │  fetch       Filter→[]Veh  │
                                     │      │                     │
                                     │      ▼                     │
                                     │  Rank        hybrid score  │
                                     └────────────┬───────────────┘
                                                  │ POST /similar
                                                  │ (8-way concurrent)
                                                  ▼
                                     ┌────────────────────────────┐
                                     │ Python embedding service   │
                                     │ (:3001)                    │
                                     │                            │
                                     │ google/embeddinggemma-2    │
                                     │ PyTorch + HuggingFace      │
                                     │ MPS on Apple Silicon       │
                                     └────────────────────────────┘
```

The Go server spawns the Python process as a child at startup, polls its `/health` endpoint, and then serves traffic. On shutdown, it reaps the child.

## API

All endpoints return `application/json`.

### `GET /health`

Returns server + embedding-service health.

```json
{
  "status": "healthy",
  "embeddings": true,
  "service": "service name",
  "version": "1.0.0",
  "db_rows": 8128
}
```

Returns `503` with an `error` field if the embedding service is unreachable.

### `POST /search/semantic`

The primary endpoint. Accepts a natural-language query, extracts constraints, filters, and ranks.

Request:

```json
{ "query": "automatic SUV under 20 lakh" }
```

Response:

```json
{
  "query": "automatic SUV under 20 lakh",
  "parsed_filter": {
    "transmission": "automatic",
    "body_type": "suv",
    "price_max": 2000000
  },
  "count": 10,
  "results": [
    {
      "id": 42,
      "name": "Mahindra XUV500 W11 AT BSIV",
      "year": 2019,
      "selling_price": 1650000,
      "km_driven": 48000,
      "fuel": "diesel",
      "transmission": "automatic",
      "seller_type": "Dealer",
      "owner": "First Owner",
      "body_type": "suv",
      "seats": 7,
      "safety_rating": 3,
      "score": 0.907
    }
  ]
}
```

### `POST /search/filter`

Structured search — same ranking pipeline but with no semantic scoring. Useful for clients that build UI filters directly.

Request:

```json
{
  "body_type": "SUV",
  "transmission": "automatic",
  "price_max": 2000000
}
```

Supports: `fuel`, `transmission`, `body_type`, `price_min`, `price_max`, `km_max`, `seats_min`, `seats_max`, `safety_rating_min`, `safety_rating_max`, `year_min`, `year_max`.

Response: `{ "filters": {...}, "count": N, "results": [...] }`

### `GET /vehicles/{id}`

Fetch a single vehicle by ID. Returns `404` if not found.

## Setup

### Prerequisites

- Go 1.22+ (uses `http.ServeMux` method+path patterns)
- Python 3.11+
- `uv` (Python package manager) — or `pip` + `venv`
- ~2 GB free disk space for the HuggingFace model cache

### Steps

```bash
# 1. Clone the repo
git clone https://github.com/ArjunSrivastava1/ai-vehicle-search.git cars24-test
cd cars24-test

# 2. Install Python dependencies
uv venv service.venv
source service.venv/bin/activate
uv pip install -r requirements.txt

# 3. Run the server — first run ingests the CSV and downloads the model
go run ./cmd/server
```

First startup takes 2–5 minutes (model download + CSV ingest). Subsequent startups take ~15 seconds (model loads from cache).

[The model is linked for its HF repository here.](https://huggingface.co/google/embeddinggemma-2)

> [!NOTE] 
> As model size is ~1.4 GB, the download will continue despite the go program timing out and progress bar will  be there, so please close the server once its completed and simply restart in a new terminal(including activating the venv), or download the model prior to usage via the link provided.
> Once cached and loaded in the system, it will be as fast as shown in the walkthrough

The server listens on `:3000`. The embedding service is spawned automatically on `:3001`.

The commands, as tested and used in the walkthrough are as follows, further in the walkthrough, a directory name that was cloned to is cars24-test:
```bash
#health check
curl -s http://localhost:3000/health | jq

# Structured filter (no semantic scoring)
curl -s -X POST http://localhost:3000/search/filter \
  -H 'Content-Type: application/json' \
  -d '{"body_type":"SUV","transmission":"automatic"}' | jq '{count, top: (.results[0:3] | map({name, transmission, body_type}))}'

# all five constraint types
curl -s -X POST http://localhost:3000/search/semantic \
  -H 'Content-Type: application/json' \
  -d '{"query":"automatic 7 seater SUV under 20 lakh from 2018"}' | jq

for q in "SUV" "diesel hatchback under 5 lakh" "5 star safety SUV" "petrol manual hatchback under 4 lakh" "maruti swift"; do
  echo "=== $q ==="
  curl -s -X POST http://localhost:3000/search/semantic \
    -H 'Content-Type: application/json' \
    -d "{\"query\":\"$q\"}" | jq '{count, top: (.results[0:2] | map(.name))}'
done
```

## Data

The dataset is `data/car_details_v3.csv` — the Kaggle Vehicle dataset (8128 rows) is publically sourced. It's committed to the repo, so no external download is needed.
[Link of the dataset on the web](https://www.kaggle.com/datasets/nehalbirla/vehicle-dataset-from-cardekho)
The dataset has multiple files, of which, car details v3 was chosen and renamed; already present in the project files therefore, once again please do not re download.

To regenerate the SQLite database from scratch:

```bash
rm cars.db
go run ./cmd/server
```

The ingest also uses `data/derived_lookup.json` to resolve `body_type` and `safety_rating` from model names (these aren't in the source CSV). Coverage: ~99.5% — 42 of 8128 rows have no body type and are searchable only by price/year/fuel/transmission.

## Configuration

Environment variables (all optional):

| Variable | Default | Purpose |
| --- | --- | --- |
| `EMBEDDING_SERVICE_URL` | `http://localhost:3001` | Embedding service base URL |
| `EMBEDDING_SERVICE_TIMEOUT` | `30s` | Per-request timeout (30s or 30000 ms) |
| `PYTHON_BIN` | `./service.venv/bin/python` | Python interpreter for the child process |
| `EMBEDDING_MODEL` | `google/embeddinggemma-2` | HuggingFace model ID |
| `EMBEDDING_SERVICE_PORT` | `3001` | Python service port |


## Design

See `DESIGN.md` for the engineering rationale: why regex for parsing, why hybrid ranking, why Go + Python, and what I'd change at scale.

Short version:

- **Regex, not an LLM, for parsing.** The domain is seven constraint types and predictable phrasing. Regex is deterministic, testable, and free.
- **Hard SQL filters for constraints, semantic scoring only within the filtered set.** A ₹25L car is wrong for "under 20 lakh" regardless of how similar it sounds.
- **Go for the API, Python for the model.** Goroutines and interfaces on one side, PyTorch and HuggingFace on the other.

## Known limitations

- **Regex-based parsing misses some phrasings:** "around 15 lakh", "over 30 lakh", "not diesel". These silently extract nothing rather than doing the wrong thing.
- **No pagination.** Results capped at 10 per query.
- **Single Python process, serialized model calls.** No batching — 100 candidates means 100 HTTP calls, not 1.
- **No auth, no rate limiting, no observability.** Out of scope for the assignment.
- **Body-type coverage is ~99.5%.** 42 rows fall through the lookup and are searchable only by non-body constraints.
