package surge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	SSEClient  *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		SSEClient: &http.Client{
			Timeout: 0,
		},
	}
}

func NewClientFromDiscovery() (*Client, error) {
	host := "127.0.0.1"
	if h := hostEnv(); h != "" {
		host = h
	}
	if p := portEnv(); p > 0 {
		return NewClient(fmt.Sprintf("http://%s:%d", host, p), ""), nil
	}
	port, ok := DiscoverPort()
	if !ok {
		return nil, fmt.Errorf("cannot discover Surge port: ensure Surge is running (surge server)")
	}
	token, _ := DiscoverToken()
	return NewClient(fmt.Sprintf("http://%s:%d", host, port), token), nil
}

func portEnv() int {
	if p := strings.TrimSpace(os.Getenv("SURGE_PORT")); p != "" {
		var port int
		if _, err := fmt.Sscanf(p, "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	return 0
}

func hostEnv() string {
	if h := strings.TrimSpace(os.Getenv("SURGE_HOST")); h != "" {
		return h
	}
	return ""
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.HTTPClient.Do(req)
}

func (c *Client) healthCheck(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return fmt.Errorf("surge unreachable at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("surge health check failed: %s", resp.Status)
	}
	return nil
}

func (c *Client) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.healthCheck(ctx)
}

func (c *Client) List(ctx context.Context) ([]DownloadStatus, error) {
	resp, err := c.do(ctx, http.MethodGet, "/list", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed: %s", resp.Status)
	}

	var statuses []DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	return statuses, nil
}

func (c *Client) History(ctx context.Context) ([]DownloadEntry, error) {
	resp, err := c.do(ctx, http.MethodGet, "/history", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("history failed: %s", resp.Status)
	}

	var entries []DownloadEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	return entries, nil
}

func (c *Client) Add(ctx context.Context, req DownloadRequest) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/download", req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result DownloadAddResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode add response: %w", err)
	}
	if result.Status == "error" {
		return "", fmt.Errorf("add failed: %s", result.Message)
	}
	return result.ID, nil
}

func (c *Client) GetStatus(ctx context.Context, id string) (*DownloadStatus, error) {
	resp, err := c.do(ctx, http.MethodGet, "/download?id="+url.QueryEscape(id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("download not found: %s", id)
		}
		return nil, fmt.Errorf("get status failed: %s", resp.Status)
	}

	var status DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	return &status, nil
}

func (c *Client) Pause(ctx context.Context, id string) error {
	return c.simpleAction(ctx, "/pause", id)
}

func (c *Client) Resume(ctx context.Context, id string) error {
	return c.simpleAction(ctx, "/resume", id)
}

func (c *Client) Delete(ctx context.Context, id string) error {
	return c.simpleAction(ctx, "/delete", id)
}

func (c *Client) Purge(ctx context.Context, id string) error {
	return c.simpleAction(ctx, "/purge", id)
}

func (c *Client) simpleAction(ctx context.Context, endpoint, id string) error {
	resp, err := c.do(ctx, http.MethodPost, endpoint+"?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s failed: %s", endpoint, id, resp.Status)
	}
	return nil
}

func (c *Client) ClearCompleted(ctx context.Context) (int64, error) {
	resp, err := c.do(ctx, http.MethodPost, "/clear-completed", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("clear completed failed: %s", resp.Status)
	}
	var result ClearResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode clear response: %w", err)
	}
	return result.Deleted, nil
}

func (c *Client) ClearFailed(ctx context.Context) (int64, error) {
	resp, err := c.do(ctx, http.MethodPost, "/clear-failed", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("clear failed failed: %s", resp.Status)
	}
	var result ClearResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode clear response: %w", err)
	}
	return result.Deleted, nil
}

func (c *Client) SetRateLimit(ctx context.Context, id string, rate int64) error {
	path := fmt.Sprintf("/rate-limit?id=%s&rate=%d", url.QueryEscape(id), rate)
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rate limit failed: %s", resp.Status)
	}
	return nil
}

func (c *Client) SetGlobalRateLimit(ctx context.Context, rate int64) error {
	path := fmt.Sprintf("/rate-limit/global?rate=%d", rate)
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("global rate limit failed: %s", resp.Status)
	}
	return nil
}

func (c *Client) StreamEvents(ctx context.Context) (<-chan SSEEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/events", nil)
	if err != nil {
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.SSEClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE connect failed: %s", resp.Status)
	}

	ch := make(chan SSEEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var current SSEEvent
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if current.Event != "" || current.Data != "" {
					select {
					case ch <- current:
					case <-ctx.Done():
						return
					}
					current = SSEEvent{}
				}
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				current.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				current.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	return ch, nil
}
