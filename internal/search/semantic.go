package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var client *http.Client
var serviceURL string

// GetServiceURL returns the embedding service URL
func GetServiceURL() string {
	return serviceURL
}

func init() {
	timeout := 30 * time.Second
	if v := os.Getenv("EMBEDDING_SERVICE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		} else if ms, err := strconv.Atoi(v); err == nil {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}

	client = &http.Client{
		Timeout: timeout,
	}

	serviceURL = os.Getenv("EMBEDDING_SERVICE_URL")
	if serviceURL == "" {
		hostname := os.Getenv("HOSTNAME")
		if hostname == "" {
			hostname = "localhost"
		}
		serviceURL = fmt.Sprintf("http://%s:3001", hostname)
	}
}

// SemanticScoreRequest represents the request to the embedding service
type SemanticScoreRequest struct {
	Query   string  `json:"query"`
	Vehicle Vehicle `json:"vehicle"`
}

// SemanticScoreResponse represents the response from the embedding service
type SemanticScoreResponse struct {
	Query      string         `json:"query"`
	Vehicle    map[string]any `json:"vehicle"`
	Similarity float64        `json:"similarity"`
	Error      string         `json:"error,omitempty"`
}

// SemanticScore returns a 0-1 score of how well the vehicle matches the query.
// Calls the embedding service (Python service with HuggingFace model) to compute cosine similarity.
func SemanticScore(v Vehicle, q string) (float64, error) {
	if serviceURL == "" {
		return 0.5, fmt.Errorf("embedding service URL not configured")
	}

	// Extract key entities from query using regex
	matches := extractQueryFeatures(q)

	// Build prompt with vehicle details and extracted query features
	prompt := buildSemanticPrompt(v, matches)

	// Prepare vehicle data for the service
	vehicleData := map[string]any{
		"name":          v.Name,
		"year":          v.Year,
		"price":         v.SellingPrice,
		"fuel":          v.Fuel,
		"transmission":  v.Transmission,
		"body_type":     v.BodyType,
		"seats":         v.Seats,
		"safety_rating": v.SafetyRating,
	}

	// Create the request payload
	body := map[string]any{
		"query":   q,
		"vehicle": vehicleData,
		"prompt":  prompt,
	}

	reqJSON, err := json.Marshal(body)
	if err != nil {
		return 0.5, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := client.Post(serviceURL+"/similar", "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		return 0.5, fmt.Errorf("failed to call embedding service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0.5, fmt.Errorf("embedding service returned status %d: %s", resp.StatusCode, string(body))
	}

	var serviceResp SemanticScoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&serviceResp); err != nil {
		return 0.5, fmt.Errorf("failed to decode embedding service response: %w", err)
	}

	// Check for errors from the service
	if serviceResp.Error != "" {
		return 0.5, fmt.Errorf("embedding service error: %s", serviceResp.Error)
	}

	// Clamp similarity to valid range [0, 1]
	score := serviceResp.Similarity
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score, nil
}

func extractQueryFeatures(q string) map[string]string {
	features := make(map[string]string)

	// Extract price range
	priceRegex := regexp.MustCompile(`(?i)(under|below|less than)\s*([\d,]+)(?:\s*(lakh|lakhs|lac|lacs|cr|crore|k|thousand))?`)
	priceMatches := priceRegex.FindStringSubmatch(q)
	if len(priceMatches) >= 3 {
		price := strings.ReplaceAll(priceMatches[2], ",", "")
		priceVal, err := strconv.ParseFloat(price, 64)
		if err != nil || priceVal <= 0 {
			// fall through; no price extracted
		} else {
			unit := ""
			if len(priceMatches) >= 4 {
				unit = strings.ToLower(priceMatches[3])
			}
			var multiplier float64
			switch unit {
			case "cr", "crore", "crores":
				multiplier = 1e7
			case "lakh", "lakhs", "lac", "lacs":
				multiplier = 1e5
			case "k", "thousand":
				multiplier = 1e3
			default:
				// No unit given. Could be raw rupees or "lakh" implied
				// by convention — but don't guess 1e5 blindly.
				multiplier = 1
			}
			features["price_max"] = fmt.Sprintf("%.0f", priceVal*multiplier)
		}
	}

	// Extract year range
	// Extract year range
	yearRegex := regexp.MustCompile(`(?i)(?:from\s*)?(\d{4})(?:\s*(?:to|-)\s*(\d{4}))?`)
	yearMatches := yearRegex.FindStringSubmatch(q)
	if len(yearMatches) >= 2 && yearMatches[1] != "" {
		if year, err := strconv.Atoi(yearMatches[1]); err == nil {
			features["year_min"] = strconv.Itoa(year)
		}
	}
	if len(yearMatches) >= 3 && yearMatches[2] != "" {
		if year, err := strconv.Atoi(yearMatches[2]); err == nil {
			features["year_max"] = strconv.Itoa(year)
		}
	}

	// Extract car type (SUV, Hatchback, Sedan, etc.)
	typeRegex := regexp.MustCompile(`(?i)(suv|hatchback|sedan|wagon|mpv|van|truck|bike)`)
	typeMatches := typeRegex.FindStringSubmatch(q)
	if len(typeMatches) >= 1 {
		features["body_type"] = strings.ToLower(typeMatches[1])
	}

	// Extract fuel type
	fuelRegex := regexp.MustCompile(`(?i)(diesel|petrol|ev|electric|hybrid|cng)`)
	fuelMatches := fuelRegex.FindStringSubmatch(q)
	if len(fuelMatches) >= 1 {
		features["fuel"] = strings.ToLower(fuelMatches[1])
	}

	// Extract transmission type
	transRegex := regexp.MustCompile(`(?i)(automatic|manual|am t)`)
	transMatches := transRegex.FindStringSubmatch(q)
	if len(transMatches) >= 1 {
		features["transmission"] = strings.ToLower(strings.ReplaceAll(transMatches[1], "am t", "amt"))
	}

	// Extract seat count
	seatRegex := regexp.MustCompile(`(?i)(\d+)\s*seat`)
	seatMatches := seatRegex.FindStringSubmatch(q)
	if len(seatMatches) >= 1 {
		if seats, err := strconv.Atoi(seatMatches[1]); err == nil {
			features["seats_min"] = strconv.Itoa(seats)
		}
	}

	// Extract safety rating
	// Extract safety rating — supports "5 star safety", "5 stars", "safety 4",
	// "safety rating 5", "safety rating 4/5", "rated 4 stars".
	safetyPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\d)\s*(?:/5|stars?)\s*safety`),                            // "5 star safety"
		regexp.MustCompile(`(?i)safety\s*(?:rating|score)?\s*(?:of\s*)?(\d)(?:\s*/\s*5)?`), // "safety 4", "safety rating 5/5"
		regexp.MustCompile(`(?i)rated\s*(\d)\s*(?:stars?|/5)`),                             // "rated 4 stars"
		regexp.MustCompile(`(?i)(\d)\s*stars?`),                                            // fallback: "5 stars"
	}
	for _, re := range safetyPatterns {
		if m := re.FindStringSubmatch(q); len(m) >= 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 5 {
				features["safety_rating_min"] = strconv.Itoa(n)
				break
			}
		}
	}

	// Check for explicit "family" keyword
	if strings.Contains(strings.ToLower(q), "family") {
		features["seats_min"] = "5" // family usually needs 5+ seats
	}

	return features
}

// buildSemanticPrompt constructs a prompt string combining vehicle details with extracted query features.
// The prompt is used by the embedding service to generate semantically similar vehicles.
func buildSemanticPrompt(v Vehicle, matches map[string]string) string {
	// Build vehicle description
	var parts []string
	parts = append(parts, fmt.Sprintf("Vehicle: %s, Year: %d, Price: ₹%d, Fuel: %s, Transmission: %s, Body Type: %s, Seats: %d, Safety Rating: %d",
		v.Name, v.Year, v.SellingPrice, v.Fuel, v.Transmission, v.BodyType, v.Seats, v.SafetyRating))

	// Add extracted query features
	if price, ok := matches["price_max"]; ok {
		parts = append(parts, fmt.Sprintf("User wants cars under ₹%s", price))
	}
	if year, ok := matches["year_min"]; ok {
		parts = append(parts, fmt.Sprintf("User wants cars from year %s", year))
	}
	if bodyType, ok := matches["body_type"]; ok {
		parts = append(parts, fmt.Sprintf("User wants %s cars", bodyType))
	}
	if fuel, ok := matches["fuel"]; ok {
		parts = append(parts, fmt.Sprintf("User wants %s cars", fuel))
	}
	if transmission, ok := matches["transmission"]; ok {
		parts = append(parts, fmt.Sprintf("User wants %s cars", transmission))
	}
	if seats, ok := matches["seats_min"]; ok {
		parts = append(parts, fmt.Sprintf("User wants cars with at least %d seats", seats))
	}
	if safety, ok := matches["safety_rating_min"]; ok {
		parts = append(parts, fmt.Sprintf("User wants cars with at least %d stars safety rating", safety))
	}

	return strings.Join(parts, ". ")
}

// The embedding service now handles semantic scoring via cosine similarity.
// This function and parseScore are no longer used - replaced by direct API call to embedding service.

func ParseQuery(q string) Filter {
	f := Filter{}
	feats := extractQueryFeatures(q)

	if v, ok := feats["price_max"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			f.PriceMax = &n
		}
	}
	if v, ok := feats["transmission"]; ok {
		f.Transmission = v
	}
	if v, ok := feats["body_type"]; ok {
		f.BodyType = v
	}
	if v, ok := feats["fuel"]; ok {
		f.Fuel = v
	}
	if v, ok := feats["seats_min"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			f.SeatsMin = &n
		}
	}
	if v, ok := feats["safety_rating_min"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			f.SafetyMin = &n
		}
	}
	if v, ok := feats["year_min"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			f.YearMin = &n
		}
	}
	if v, ok := feats["year_max"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			f.YearMax = &n
		}
	}

	return f
}
