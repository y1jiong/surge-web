package surge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	host := DiscoverHost()
	port, ok := DiscoverPort()
	if !ok {
		return nil, fmt.Errorf("cannot discover Surge port: ensure Surge is running (surge server)")
	}
	token, _ := DiscoverToken()
	return NewClient(fmt.Sprintf("http://%s:%d", host, port), token), nil
}

func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func (c *Client) NewSSERequest(ctx context.Context) (*http.Request, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/events")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := c.newRequest(ctx, method, path)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Body = io.NopCloser(bodyReader)
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
