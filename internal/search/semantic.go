package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var client *http.Client
var apiKey string

func init() {
	client = &http.Client{}
	apiKey = os.Getenv("OPENAI_API_KEY")
}

// SemanticScore returns a 0-1 score of how well the vehicle matches the query.
// Uses OpenAI-compatible LLM API to score based on natural language description.
func SemanticScore(v Vehicle, q string) (float64, error) {
	if apiKey == "" {
		return 0.5, nil // neutral score if no API key
	}

	// Extract key entities from query using regex
	matches := extractQueryFeatures(q)

	// Build prompt with vehicle details and extracted query features
	prompt := buildSemanticPrompt(v, matches)

	body := map[string]any{
		"model":         "gpt-4o-mini",
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are a car matching assistant. Return only a single float number between 0 and 1, nothing else.",
			},
			{
				"role": "user",
				"content": prompt,
			},
		},
		"temperature": 0.0,
	}

	req, err := json.Marshal(body)
	if err != nil {
		return 0.5, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqStr := string(req)

	url := os.Getenv("OPENAI_API_URL")
	if url == "" {
		url = "https://api.openai.com/v1/chat/completions"
	}

	resp, err := client.Post(url, "application/json", strings.NewReader(reqStr))
	if err != nil {
		return 0.5, fmt.Errorf("failed to call LLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0.5, fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	var llmResp struct {
		Choice struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choice"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return 0.5, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	// Parse the float score from response
	score := parseScore(llmResp.Choice.Message.Content)
	if score < 0 || score > 1 {
		score = 0.5 // clamp to valid range
	}

	return score, nil
}

func extractQueryFeatures(q string) map[string]string {
	features := make(map[string]string)

	// Extract price range
	priceRegex := regexp.MustCompile(`(under|below|less than)\s*([\\d,]+)(?:\s*(?:lakh|lakhs|k|thousand|cr))?`)
	priceMatches := priceRegex.FindStringSubmatch(q)
	if len(priceMatches) >= 3 {
		price := strings.ReplaceAll(priceMatches[2], ",", "")
		priceVal, _ := strconv.ParseFloat(price, 64)
		if priceVal > 0 {
			features["price_max"] = fmt.Sprintf("%d", priceVal*100000) // convert lakhs to rupees
		}
	}

	// Extract year range
	yearRegex := regexp.MustCompile(`(new|recent|latest|from\s*([\\d]{4})\s*to?\s*([\\d]{4}))`)
	yearMatches := yearRegex.FindStringSubmatch(q)
	if len(yearMatches) >= 3 && yearMatches[2] != "" {
		if year, err := strconv.Atoi(yearMatches[2]); err == nil {
			features["year_min"] = strconv.Itoa(year)
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
	safetyRegex := regexp.MustCompile(`(?i)safety\s*(rating|score)\s*(?:[:/ ]|\d+/\d+)`)
	safetyMatches := safetyRegex.FindStringSubmatch(q)
	if len(safetyMatches) >= 1 {
		safetyStr := strings.TrimSpace(safetyMatches[1])
		// Handle "4/5 stars" format
		if strings.Contains(safetyStr, "/") {
			parts := strings.Split(safetyStr, "/")
			if len(parts) == 2 {
				if num, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
					features["safety_rating_min"] = strconv.Itoa(num)
				}
			}
		} else if num, err := strconv.Atoi(safetyStr); err == nil {
			features["safety_rating_min"] = strconv.Itoa(num)
		}
	}

	// Check for explicit "family" keyword
	if strings.Contains(strings.ToLower(q), "family") {
		features["seats_min"] = "5" // family usually needs 5+ seats
	}

	return features
}

func buildSemanticPrompt(v Vehicle, features map[string]string) string {
	var sb strings.Builder

	sb.WriteString("Given this car and a user's text query, score how well they match on a scale from 0 (no match) to 1 (perfect match).\n\n")

	sb.WriteString("Car details:\n")
	sb.WriteString(fmt.Sprintf("- Name: %s\n", v.Name))
	sb.WriteString(fmt.Sprintf("- Year: %d\n", v.Year))
	sb.WriteString(fmt.Sprintf("- Price: ₹%d\n", v.SellingPrice))
	sb.WriteString(fmt.Sprintf("- Fuel: %s\n", v.Fuel))
	sb.WriteString(fmt.Sprintf("- Transmission: %s\n", v.Transmission))
	sb.WriteString(fmt.Sprintf("- Body Type: %s\n", v.BodyType))
	sb.WriteString(fmt.Sprintf("- Seats: %d\n", v.Seats))
	sb.WriteString(fmt.Sprintf("- Safety Rating: %d\n", v.SafetyRating))

	sb.WriteString("\nUser query: ")
	sb.WriteString(v.Name)
	sb.WriteString(" ")
	sb.WriteString(features["query"])

	sb.WriteString("\n\n\nMatch the car to the query based on: " + buildMatchCriteria(features) + "\n")
	sb.WriteString("Return ONLY a single float number (0.0 to 1.0), nothing else.")

	return sb.String()
}

func buildMatchCriteria(features map[string]string) string {
	criteria := make([]string, 0, len(features))

	for key, value := range features {
		if value == "" {
			continue
		}
		criteria = append(criteria, fmt.Sprintf("- %s must be %s", key, value))
	}

	return strings.Join(criteria, ", ")
}

func parseScore(content string) float64 {
	// Try to extract float from response
	re := regexp.MustCompile(`(-?[\d,]+\.?\d*)`)
	matches := re.FindString(content)

	if matches != "" {
		// Clean the number (remove commas)
		cleaned := strings.ReplaceAll(matches, ",", "")
		if num, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return num
		}
	}

	// Fallback: check for common score representations
	lowerContent := strings.ToLower(content)

	if strings.Contains(lowerContent, "1") && !strings.Contains(lowerContent, "0") {
		return 1.0
	}
	if strings.Contains(lowerContent, "0") && !strings.Contains(lowerContent, "1") {
		return 0.0
	}
	if strings.Contains(lowerContent, "yes") {
		return 1.0
	}
	if strings.Contains(lowerContent, "no") {
		return 0.0
	}
	if strings.Contains(lowerContent, "perfect") || strings.Contains(lowerContent, "excellent") || strings.Contains(lowerContent, "great") {
		return 0.9
	}
	if strings.Contains(lowerContent, "close") || strings.Contains(lowerContent, "somewhat") {
		return 0.6
	}
	if strings.Contains(lowerContent, "not") {
		return 0.3
	}

	// Default to 0.5 if we can't parse
	return 0.5
}
