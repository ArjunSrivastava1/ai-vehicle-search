# Design

Engineering rationale for `ai-vehicle-search`. This document explains *why* the
system is shaped the way it is — the decisions, the tradeoffs, and what I'd
change at scale.

---

## 1. Problem interpretation

The assignment gives three example queries:

- *"Show SUVs under 15L"*
- *"Diesel automatic cars below 80k km"*
- *"Family cars with high safety ratings"*

These aren't a single class of problem. They mix:

- **Hard constraints** — "under 15L", "below 80k km" are numeric bounds. A ₹20L
  car is wrong, not just ranked lower.
- **Categorical filters** — "SUV", "diesel", "automatic". A petrol car is wrong.
- **Vague preferences** — "family cars", "high safety". These are fuzzy. There's
  no exact answer; there's a better or worse match.

A useful implementation has to handle all three, and treat them differently. The
natural-language query needs to become a *structured filter* for the first two
classes, and a *relevance signal* for the third. Doing either alone produces a
system that's obviously broken — a pure embedding search ignores "under 15L"; a
pure SQL filter can't rank "family cars" by quality.

So the design splits the problem:

1. **Parse** the query into a `Filter` struct with typed fields.
2. **Filter** the catalogue in SQL — hard constraints applied as `WHERE`.
3. **Rank** the survivors with a hybrid score that includes semantic similarity.

This split is the core architectural decision. Everything below follows from it.

---

## 2. Architecture

Two services:

- **Go HTTP server** on `:3000` — the API, query parsing, SQL filtering, ranking.
- **Python embedding service** on `:3001` — a HuggingFace model that returns
  cosine similarity between a query and a vehicle description.

The Go server spawns the Python process at startup, polls `/health`, and reaps
it on shutdown. This is the "single command to run everything" tradeoff.

### Why two languages

**Go for the API** — the web layer wants goroutines, `http.ServeMux` with method
patterns, and a single static binary. The concurrency model matters here: the
semantic scoring fan-out is the only interesting piece of parallel work in the
system, and Go makes it idiomatic (bounded worker pool via a buffered channel
as a semaphore).

**Python for the model** — PyTorch, HuggingFace, and the ML ecosystem live in
Python. Reimplementing model loading and tokenization in Go would be weeks of
work to end up with something worse.

**Split vs. monolithic.** - A single Go binary that embeds a model via CGo is
possible but painful (build complexity, no GPU acceleration on MPS through CGo).
A single Python service with FastAPI would put all the HTTP handling and SQL
logic in Python, which I wanted to avoid thus Go's stdlib HTTP and database/sql are
better fits than Python's equivalents for this kind of service.

The tradeoff is operational complexity.

> [!NOTE]
> Seperation of both the models, and the services also provides the benefit of debugging the two seperately, or running the embedding service independent of each the go program, which is extremely useful as it means that if only the model is problematic, to be changed, or any other improvements, it can be tested and ran seperately.
> It also shows both the frameworks running in their best suited environments, and performing extremely well, one in pythons and transformers while the other in go processes.

Ofcourse, please be careful of port conflict errors, running the two seperately while possible is also to be careful to not spin them both at the same time.

---

## 3. Query understanding

### The decision: regex, not an LLM

The obvious alternative to a regex parser is to send the query to an LLM
(OpenAI, Claude, a local model) and ask it to return JSON. That would handle
"around 15 lakh", "not diesel", and every other phrasing I haven't thought of.

I chose regex anyway. The reasoning:

**The domain is narrow.** Seven constraint types: `body_type`, `fuel`,
`transmission`, `seats_min`, `price_max`, `year_min`/`year_max`,
`safety_rating_min`. The queries users write for car search are highly repetitive.
Regex covers the common cases completely.

**Determinism matters more than coverage.** If the parser returns different
results for the same input on two runs, the whole system becomes untestable and
unreproducible. An LLM introduces non-determinism at the most sensitive layer.
With regex, "under 10 lakh" *always* means ₹1,000,000, and I can write a test
that asserts exactly that.

**Latency.** A regex parse is microseconds. An LLM call is 200ms–2s and requires
a network. For a search-as-you-type interface, that's the difference between
responsive and broken.

**Cost.** Zero. No API keys, no rate limits, no data leaving the machine.

**What this gives up:** coverage of unusual phrasings. "Around 15 lakh" doesn't
extract a constraint — it silently returns results at any price. That's a real
gap. My position is that it's better to *fail silently to no constraint* than to
*fail to a wrong constraint*. An LLM that hallucinates "price_max: 15000000"
when the user said "around 15 lakh" is worse than one that says nothing.

The parser is structured so an LLM could be added as a fallback: if the regex
extracts nothing but the query looks like it contains constraints, call the LLM.
I didn't build that, but the seams are there (`ParseQuery` is a single function;
its internals can change).

### What it extracts

Each constraint has a dedicated regex. Example, price:

```go
priceRegex := regexp.MustCompile(`(?i)(under|below|less than)\s*([\d,]+)(?:\s*(lakh|lakhs|lac|lacs|cr|crore|k|thousand))?`)
```

- `under|below|less than` — three common phrasings for "max price"
- `[\d,]+` — digits with optional comma separators ("10,00,000" and "10 lakh")
- `(lakh|cr|k|...)` — unit, which determines the multiplier (`1e5`, `1e7`, `1e3`)

Unit handling is not trivial: "under 10 lakh" → 1,000,000, "under 5 cr" →
50,000,000, "under 20k" → 20,000. The parser captures the unit and applies the
right multiplier. Getting this wrong was one of the real bugs during
development.

### The output

`ParseQuery` returns a `Filter` struct with pointer fields for numerics (so
"not specified" is distinguishable from "zero"):

```go
type Filter struct {
    Fuel         string
    Transmission string
    BodyType     string
    PriceMax     *int
    YearMin      *int
    YearMax      *int
    SeatsMin     *int
    SafetyMin    *int
    // ...
}
```

This struct is the contract between the parser and the rest of the system. Once
the query is a `Filter`, everything downstream is deterministic.

---

## 4. Filtering strategy

### Hard constraints are hard

Every constraint the parser extracts becomes a `WHERE` clause:

```go
if f.PriceMax != nil {
    where = append(where, "selling_price <= ?")
    args = append(args, *f.PriceMax)
}
```

A ₹25L car is excluded from "under 20 lakh" queries. This is intentional and
important. The failure mode I wanted to avoid:

> User: "under 20 lakh"
> System: returns a ₹25L car with a high semantic score
> User: "this is broken"

Hybrid systems often make this mistake. They let semantic similarity carry the
ranking and treat constraints as soft preferences. It produces results that are
*usually* close but *sometimes* wrong, which is worse than being consistently
strict.

### Case-insensitive comparison

The database has `body_type: "SUV"` (capitalized). The parser produces `"suv"`
(lowercase). Raw `WHERE body_type = ?` fails to match.

Two fixes are available:

1. `LOWER(body_type) = ?` — works but defeats indexes.
2. `COLLATE NOCASE` — SQLite-specific, keeps the index usable.

I chose `COLLATE NOCASE`. It's explicit in the SQL, per-comparison, and doesn't
sacrifice index performance. An alternative would be normalizing at ingest time,
which I should also do but haven't gotten to.

### AMT counts as automatic

The dataset has three transmission values: `Manual`, `Automatic`, `AMT`. AMT is
an automated manual — mechanically a manual, but driven without a clutch pedal.
Users typing "automatic" mean both.

The `fetch` function special-cases this:

```go
if strings.EqualFold(f.Transmission, "automatic") {
    where = append(where, "(transmission = 'automatic' COLLATE NOCASE OR transmission = 'amt' COLLATE NOCASE)")
}
```

The alternative is to normalize AMT → automatic at ingest and lose the
distinction. I kept the distinction because a user searching for AMT specifically
(the cheaper variant) should still be able to.

### Unresolved rows are searchable

The source CSV doesn't have `body_type` or `safety_rating`. These come from a
lookup table (`data/derived_lookup.json`) that maps model names to attributes.
~99.5% of rows match. The other 0.5% (42 rows) have `body_type = NULL`.

An earlier version excluded unresolved rows entirely:

```sql
WHERE body_source != 'unresolved'
```

That silently dropped 42 cars from every result. Worse: if the lookup coverage
had been 70% instead of 99.5%, it would have dropped 2400 cars. The filter is now
gone — unresolved rows appear in non-body-type queries and are excluded from
body-type queries naturally, because SQL `WHERE body_type = 'suv'` doesn't match
`NULL`. This is the right behavior: don't drop data that doesn't match a specific
constraint; let the constraint do its job.

---

## 5. Ranking

Within the filtered candidate set, each vehicle gets a composite score:

```go
v.Score = wExact     * exactMatch(v, f)     +
          wPrice     * priceProximity(v, f) +
          wFreshness * yearMatch(v, f)      +
          wSemantic  * semanticScore(v, q)
```

Weights: `0.35` exact, `0.25` price, `0.25` semantic, `0.15` freshness.

These are tuned by intuition, not by an evaluation set. A real system would
learn them from click data. For a demo, "roughly balanced, semantic gets a
quarter" is defensible.

### Why hybrid, not pure semantic

If semantic similarity is the only ranking signal:

- Constraints have to be encoded in the query embedding, which models are bad at
  for numeric bounds.
- A ₹20L car might outrank a ₹18L car for "under 20 lakh" because it sounds
  more "premium".
- There's no way to say "I definitely want diesel" without it being a soft
  preference the embedding can ignore.

### Why hybrid, not pure structured

If structured match is the only signal:

- All 7-seat diesel SUVs under 20L rank identically. There's no quality order.
- "Family car" and "good safety" don't parse to constraints at all — they'd
  return everything.
- The system can't use the natural-language signal, which was the whole point.

### What each term does

**`exactMatch`** — fraction of specified categorical constraints satisfied.
For "diesel automatic SUV", if a vehicle is diesel+automatic+SUV, score 1.0;
if only diesel+automatic, 0.67. Since filters are applied in SQL, this should
*always* be 1.0 for surviving candidates. It's kept as a signal because (a)
some constraints aren't in SQL (safety_rating at time of writing — since fixed),
(b) it makes the weighting explicit rather than implicit.

**`priceProximity`** — for "under 15 lakh", cheaper within the budget scores
higher (closer to 0 on the ratio, closer to 1 on the score). This prefers the
₹8L car over the ₹14.5L car for the same budget, which is arguably what users
want when they say "under X" — they want affordable, not "just barely under."

**`yearMatch`** — soft preference for newer cars when no year constraint is
specified. If a year range is specified, it's now applied in SQL and this term
scores 1.0 for all survivors. (Earlier versions kept it as a scoring term even
with constraints, which caused 2015 Fortuners to outrank 2019 XUV500s for
"from 2018" — a bug I fixed by promoting year to a hard filter.)

**`semanticScore`** — cosine similarity between the query embedding and a
natural-language description of the vehicle (built from its fields). Captures
things the regex can't: "premium", "sporty", "family", "rugged". This is the
"AI" part of "AI vehicle search" — the piece that adds genuine signal beyond
what SQL can express.

---

## 6. Semantic scoring

### Model choice

`google/embeddinggemma-2` — a recent open-source text embedding model from
Google. Chosen because:

- Small enough to run on MPS/consumer hardware (~1.5 GB).
- Strong on short-text semantic similarity.
- Doesn't require an API key or network dependency at inference time.

gemma 2 embeddings model also ensures that for features such as voice searches, models dont have to be changed, since its a multimodal embeddings model, thus, it ensures simplicity and flexibility for additions.

Alternatives considered: OpenAI's `text-embedding-3-small` (fast, cheap, but
requires network), `sentence-transformers/all-MiniLM-L6-v2` (smaller, faster,
weaker on automotive domain). Gemma-2 won on the "runs locally + good enough"
axis.

### Embedding at query time, not ingest

Two approaches to semantic search:

**A. Precompute embeddings at ingest.** Each vehicle gets an embedding stored
in the DB. Query is embedded once, similarity is a dot product over the
precomputed matrix. Fast (milliseconds) but requires a vector index at scale
(FAISS, pgvector) and doesn't work if you want to rank a candidate set that's
already filtered by SQL.

**B. Embed at query time.** The query and each candidate's description are
embedded on the fly, cosine similarity computed. Slower (~260ms per vehicle
here) but composes trivially with SQL filtering — you only embed the surviving
candidates, not the whole catalogue.

I chose B because the system is fundamentally filter-then-rank, not
rank-then-filter. If you filter first, you only embed the 100 survivors, not
8128. The performance cost (~4s for the broadest query) is acceptable for a
demo, and the architecture composes with structured filtering in a way that A
doesn't.

**A future optimization:** cache query embeddings. In a search UI, the same
query gets hit repeatedly as the user refines filters. Caching (query →
embedding) means the first request pays the tokenization cost and subsequent
requests only pay the dot-product. That alone would cut the broad-query latency
roughly in half for repeat queries. Not built, but a 20-line change.

### Similarity function

Cosine similarity between the two embedding vectors. Range `[-1, 1]`, clamped
to `[0, 1]` on return. In practice, embeddings rarely go negative for
semantically-related text, so scores are clustered in `[0.4, 0.95]` for most
vehicle-query pairs. That's fine — relative ordering is what matters.

The raw value would be misleading if compared across queries. Don't read
"0.91" as "91% match" — read it as "ranked above everything with 0.90 or lower
for this query." A future improvement would be to normalize per-query
(z-score or min-max the scores across candidates) to make the values interpretable
in the UI.

---

## 7. Concurrency

The semantic scoring is the only I/O-bound, potentially-parallel piece of work.
For a query that returns 100 candidates, naive sequential scoring means 100
HTTP calls to Python, ~260ms each = 26 seconds.

### Go side: bounded worker pool

```go
sem := make(chan struct{}, 8)   // 8 concurrent requests
var wg sync.WaitGroup
for i := range results {
    wg.Add(1)
    sem <- struct{}{}            // blocks when full
    go func(i int) {
        defer wg.Done()
        defer func() { <-sem }() // release
        score, err := SemanticScore(results[i], query)
        // ...
        semScores[i] = score     // no lock — each index is unique
    }(i)
}
wg.Wait()
```

Key details:

- **`sem` as a semaphore.** A buffered channel used as a concurrency limiter.
  This is the idiomatic Go pattern. Alternatives (worker pool, `errgroup`) work
  but are more code for the same result.
- **`wg.Add(1)` before `sem <-`.** If done in the other order, `wg.Wait()` could
  return before the last goroutine started, and we'd read `semScores` with an
  unpopulated slot.
- **Write to `semScores[i]`, not `results[i].Score`.** Each goroutine touches a
  different index of a pre-allocated slice, so there's no data race. The
  composite scoring happens *after* `wg.Wait()` in a sequential loop. This is
  the clean way to write concurrent code — a data race is prevented by
  construction, not by a mutex.

Measured improvement: ~7× on a query with 500 candidates. Not 8× because the
Python side serializes.

### Python side: threading + inference lock

`HTTPServer` handles one request at a time. `ThreadingHTTPServer` handles
requests on threads. But PyTorch models aren't reliably thread-safe for
concurrent `forward()` calls, especially on MPS.

Solution: accept HTTP requests concurrently (threading server), but serialize
the model call with a lock:

```python
def encode(self, text):
    with self._inference_lock:
        inputs = self.processor(text, ...)
        with torch.no_grad():
            embeddings = self.model(**inputs).last_hidden_state
        # ... pooling
        result = embedding.tolist()
    return result
```

The lock is held only during the forward pass and pooling. `result.tolist()`
copies to CPU, so the returned value is safe to use outside the lock.

Result: 8 concurrent Go requests hit the Python service; the HTTP layer accepts
all 8; the model runs them one at a time. The 7× speedup comes from overlapping
the *overhead* (HTTP parsing, tokenization, JSON serialization), not the model
forward pass.

**The honest number:** if you send 100 requests at 8-way concurrency to a model
that takes 15ms per forward pass plus 240ms of overhead, sequential = 25.5s,
parallel = 3.6s. The 240ms of overhead is what parallelizes; the 15ms of compute
doesn't. This is why the batch endpoint (one HTTP call for N vehicles) is the
real optimization — it eliminates the overhead entirely, not just overlaps it.

---

## 8. Data pipeline

### Source

Kaggle *Car Price Prediction* dataset (`car details v3.csv`), 8128 rows. Real
Indian used-car listings with name, year, price, km_driven, fuel, seller_type,
transmission, owner. What it doesn't have: `body_type` and `safety_rating`.

### Derived attributes

A hand-curated lookup (`data/derived_lookup.json`) maps model name substrings to
`{body_type, seats, safety_rating}`. `"safari"` → `{SUV, 7, 5}`,
`"swift"` → `{Hatchback, 5, 3}`.

Name matching is normalized (`normalizeName`): lowercase, strip spaces/hyphens/
underscores. So `"Maruti Wagon R LXI"` matches lookup key `"wagon-r"`. Longest
match wins — `"xuv700"` beats `"xuv"` for a name like `"Mahindra XUV700 AXE"`.

### Coverage

~99.5% at the row level: 42 of 8128 rows don't match any lookup key. These are
long-tail models (older Tata Indigo variants, some discontinued imports). They're
still in the database and searchable by every constraint except body_type/safety.

Why not 100%? The lookup was hand-curated from the earlier 155-row dataset. The
8128-row file has more models than the original lookup anticipated. Adding keys
for the top misses (`"ecosport"`, `"wrx"`, `"thar"`) got coverage from 54% to
82%; most of the remaining gap is one-off variants. Diminishing returns to
extend it further.

### Seats from CSV, body_type from lookup

Interesting wrinkle: the CSV *does* have a `seats` column, but not `body_type`.
So the loader prefers CSV `seats` when present, falls back to lookup. This
improved coverage significantly (from ~82% to ~99.5%) because many vehicles match
on `seats` but not on `body_type`.

### Deduplication

The Kaggle dataset contains duplicate rows (same listing, scraped multiple times).
Without dedup, the top 3 results for "SUV" were three copies of the same Jeep
Wrangler. Fixed with `GROUP BY` on the visible fields in the SQL query:

```sql
SELECT MIN(id), name, year, selling_price, ... FROM vehicles
WHERE ...
GROUP BY name, year, selling_price, km_driven, fuel, transmission,
         seller_type, owner, body_type, seats, safety_rating
```

`MIN(id)` is arbitrary but stable as the deduplicated result has a valid ID.

---
### Tests

There are no unit tests. For a system this size, that's a real gap. Priority
tests:

- `extractQueryFeatures` on ~30 representative queries (table-driven test).
- `Rank` on a fixed candidate set with known scores (verifies the ranking math
  didn't change when weights are tuned).
- `fetch` with various filter combinations (verifies SQL construction).

These would catch regressions that manual testing misses. The reason they're not
there: time. The reason they'd be first on my list to add: without tests, every
change to the parser or ranker carries risk.

---

## 10. Summary of tradeoffs

| Decision | Alternative | Why this |
|---|---|---|
| Regex parsing | LLM parsing | Deterministic, fast, free, testable |
| Hard SQL filters | Soft scoring | Correctness: wrong constraints are worse than missing constraints |
| Hybrid ranking | Pure semantic | Semantic can't express numeric bounds; structured can't express vague preferences |
| Embed at query time | Embed at ingest | Composes with SQL filters; avoids vector index |
| Go + Python | Single language | Each does what it's good at; small complexity cost |
| Spawn Python as subprocess | Separate containers | One command to run; no orchestration for a demo |
| No batch endpoint | Batch now | Acceptable latency for assignment scope; clear future improvement |
| No tests | Write tests now | Real gap; time constraint; highest priority for future work |
