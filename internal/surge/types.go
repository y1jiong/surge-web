package surge

type DownloadRequest struct {
	URL                   string            `json:"url"`
	Filename              string            `json:"filename,omitempty"`
	Path                  string            `json:"path,omitempty"`
	RelativeToDefaultDir  bool              `json:"relative_to_default_dir,omitempty"`
	Mirrors               []string          `json:"mirrors,omitempty"`
	SkipApproval          bool              `json:"skip_approval,omitempty"`
	Headers               map[string]string `json:"headers,omitempty"`
	IsExplicitCategory    bool              `json:"is_explicit_category,omitempty"`
	Workers               int               `json:"workers,omitempty"`
	MinChunkSize          int64             `json:"min_chunk_size,omitempty"`
}

type BatchDownloadRequest struct {
	Downloads    []DownloadRequest `json:"downloads"`
	Path         string            `json:"path,omitempty"`
	SkipApproval bool              `json:"skip_approval,omitempty"`
}

type DownloadAddResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type BatchAddResponse struct {
	Status   string              `json:"status"`
	Message  string              `json:"message"`
	Count    int                 `json:"count,omitempty"`
	Failures []BatchAddFailure   `json:"failures,omitempty"`
}

type BatchAddFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

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

type ActionResponse struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

type ClearResponse struct {
	Deleted int64 `json:"deleted"`
}

type RateLimitResponse struct {
	Status string `json:"status"`
	ID     string `json:"id,omitempty"`
	Rate   string `json:"rate,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
	Port   int    `json:"port"`
}

type SSEEvent struct {
	Event string `json:"-"`
	Data  string `json:"-"`
}
