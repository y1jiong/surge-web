package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"surge-web/internal/surge"
)

type Proxy struct {
	Client *surge.Client
	Token  string
	Logger *log.Logger
}

func NewProxy(client *surge.Client, logger *log.Logger) *Proxy {
	return &Proxy{
		Client: client,
		Token:  client.Token,
		Logger: logger,
	}
}

func (p *Proxy) ProxyAPI(w http.ResponseWriter, r *http.Request) {
	targetPath := strings.TrimPrefix(r.URL.Path, "/api")
	if targetPath == "" {
		targetPath = "/"
	}

	targetURL := p.Client.BaseURL + targetPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	var body io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, body)
	if err != nil {
		http.Error(w, "proxy error", http.StatusInternalServerError)
		p.Logger.Printf("proxy create request error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}

	resp, err := p.Client.HTTPClient.Do(req)
	if err != nil {
		http.Error(w, "surge unreachable", http.StatusBadGateway)
		p.Logger.Printf("proxy request error: %v", err)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, val := range values {
			w.Header().Add(key, val)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *Proxy) ProxySSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.Client.BaseURL+"/events", nil)
	if err != nil {
		http.Error(w, "sse error", http.StatusInternalServerError)
		return
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := p.Client.SSEClient.Do(req)
	if err != nil {
		http.Error(w, "surge unreachable", http.StatusBadGateway)
		p.Logger.Printf("SSE connect error: %v", err)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (p *Proxy) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (p *Proxy) writeError(w http.ResponseWriter, status int, message string) {
	p.writeJSON(w, status, map[string]string{"status": "error", "message": message})
}

func (p *Proxy) HandleGetToken(w http.ResponseWriter, r *http.Request) {
	p.writeJSON(w, http.StatusOK, map[string]string{
		"token":   p.Token,
		"baseURL": p.Client.BaseURL,
	})
}

func (p *Proxy) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/", p.ProxyAPI)
	mux.HandleFunc("/events", p.ProxySSE)
	mux.HandleFunc("/api/token", p.HandleGetToken)

	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/files/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			p.writeError(w, http.StatusBadRequest, "missing download id")
			return
		}
		id := parts[0]
		p.serveFile(w, r, id)
	})

	return mux
}

func (p *Proxy) serveFile(w http.ResponseWriter, r *http.Request, id string) {
	status, err := p.Client.GetStatus(r.Context(), id)
	if err != nil {
		entries, herr := p.Client.History(r.Context())
		if herr != nil {
			p.writeError(w, http.StatusNotFound, "download not found")
			return
		}
		for _, e := range entries {
			if e.ID == id {
				p.streamFile(w, r, e.DestPath, e.Filename)
				return
			}
		}
		p.writeError(w, http.StatusNotFound, "download not found")
		return
	}
	p.streamFile(w, r, status.DestPath, status.Filename)
}

func (p *Proxy) streamFile(w http.ResponseWriter, r *http.Request, destPath, filename string) {
	if destPath == "" {
		p.writeError(w, http.StatusNotFound, "file path not available")
		return
	}

	if filename == "" {
		cleaned := strings.TrimPrefix(destPath, "/")
		if idx := strings.LastIndex(cleaned, "/"); idx >= 0 {
			filename = cleaned[idx+1:]
		} else {
			filename = cleaned
		}
	}

	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	http.ServeFile(w, r, destPath)
}
