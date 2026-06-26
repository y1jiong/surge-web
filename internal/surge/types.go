package surge

type DownloadStatus struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Filename     string  `json:"filename"`
	DestPath     string  `json:"dest_path,omitempty"`
	TotalSize    int64   `json:"total_size"`
	Downloaded   int64   `json:"downloaded"`
	Progress     float64 `json:"progress"`
	Speed        float64 `json:"speed"`
	Status       string  `json:"status"`
	Error        string  `json:"error,omitempty"`
	ETA          int64   `json:"eta"`
	Connections  int     `json:"connections"`
	AddedAt      int64   `json:"added_at"`
	TimeTaken    int64   `json:"time_taken"`
	AvgSpeed     float64 `json:"avg_speed"`
	RateLimit    int64   `json:"rate_limit,omitempty"`
	RateLimitSet bool    `json:"rate_limit_set,omitempty"`
}

type DownloadEntry struct {
	ID          string   `json:"id"`
	URLHash     string   `json:"url_hash"`
	URL         string   `json:"url"`
	DestPath    string   `json:"dest_path"`
	Filename    string   `json:"filename"`
	Status      string   `json:"status"`
	TotalSize   int64    `json:"total_size"`
	Downloaded  int64    `json:"downloaded"`
	CompletedAt int64    `json:"completed_at"`
	TimeTaken   int64    `json:"time_taken"`
	AvgSpeed    float64  `json:"avg_speed"`
	Mirrors     []string `json:"mirrors,omitempty"`
}
