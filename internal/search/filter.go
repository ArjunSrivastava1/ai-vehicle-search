package search

type Filter struct {
	Fuel         string `json:"fuel,omitempty"`
	Transmission string `json:"transmission,omitempty"`
	BodyType     string `json:"body_type,omitempty"`
	PriceMin     *int   `json:"price_min,omitempty"`
	PriceMax     *int   `json:"price_max,omitempty"`
	KMMax        *int   `json:"km_max,omitempty"`
	SeatsMin     *int   `json:"seats_min,omitempty"`
	SafetyMin    *int   `json:"safety_rating_min,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type Vehicle struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Year          int    `json:"year"`
	SellingPrice  int    `json:"selling_price"`
	KMDriven      int    `json:"km_driven"`
	Fuel          string `json:"fuel"`
	Transmission  string `json:"transmission"`
	SellerType    string `json:"seller_type,omitempty"`
	Owner         string `json:"owner,omitempty"`
	BodyType      string `json:"body_type,omitempty"`
	Seats         int    `json:"seats,omitempty"`
	SafetyRating  int    `json:"safety_rating,omitempty"`
	Score         float64 `json:"score"`
}
