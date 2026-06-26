package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"surge-web/internal/surge"
)

type Proxy struct {
	mu     sync.RWMutex
	client *surge.Client
	Logger *log.Logger
}

func NewProxy(logger *log.Logger) *Proxy {
	return &Proxy{Logger: logger}
}

func (p *Proxy) SetClient(c *surge.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client = c
}

func (p *Proxy) GetClient() *surge.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client
}

func (p *Proxy) HandleStatus(w http.ResponseWriter, r *http.Request) {
	c := p.GetClient()
	connected := c != nil && c.BaseURL != ""
	p.writeJSON(w, http.StatusOK, map[string]interface{}{
		"connected": connected,
	})
}

func (p *Proxy) ProxyAPI(w http.ResponseWriter, r *http.Request) {
	c := p.GetClient()
	if c == nil || c.BaseURL == "" {
		p.writeError(w, http.StatusServiceUnavailable, "surge not connected")
		return
	}

	targetPath := strings.TrimPrefix(r.URL.Path, "/api")
	if targetPath == "" {
		targetPath = "/"
	}

	targetURL := c.BaseURL + targetPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	var body io.Reader
	if r.ContentLength > 0 && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, body)
	if err != nil {
		http.Error(w, "proxy error", http.StatusInternalServerError)
		p.Logger.Printf("proxy create request error: %v", err)
		return
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		http.Error(w, "surge unreachable", http.StatusBadGateway)
		p.Logger.Printf("proxy request error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		p.Logger.Printf("auth token rejected by surge, clearing client to trigger re-discovery")
		p.SetClient(nil)
		http.Error(w, "surge authentication failed – reconnecting", http.StatusBadGateway)
		return
	}

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
	c := p.GetClient()
	if c == nil || c.BaseURL == "" {
		http.Error(w, "surge not connected", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	req, err := c.NewSSERequest(r.Context())
	if err != nil {
		http.Error(w, "sse error", http.StatusInternalServerError)
		return
	}

	resp, err := c.SSEClient.Do(req)
	if err != nil {
		http.Error(w, "surge unreachable", http.StatusBadGateway)
		p.Logger.Printf("sse connect error: %v", err)
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

func (p *Proxy) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", p.HandleStatus)
	mux.HandleFunc("/api/", p.ProxyAPI)
	mux.HandleFunc("/events", p.ProxySSE)

	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/file/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			p.writeError(w, http.StatusBadRequest, "missing download id")
			return
		}
		id := parts[0]
		if len(parts) > 1 && parts[1] == "encrypt" {
			p.serveEncryptedFile(w, r, id)
			return
		}
		p.serveFile(w, r, id)
	})

	return mux
}

func (p *Proxy) serveFile(w http.ResponseWriter, r *http.Request, id string) {
	c := p.GetClient()
	if c == nil || c.BaseURL == "" {
		p.writeError(w, http.StatusServiceUnavailable, "surge not connected")
		return
	}

	status, err := c.GetStatus(r.Context(), id)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			p.writeError(w, http.StatusBadGateway, "surge request failed")
			return
		}
		entries, herr := c.History(r.Context())
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
		filename = filepath.Base(destPath)
	}

	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.QueryEscape(filename))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	http.ServeFile(w, r, filepath.Clean(destPath))
}

func (p *Proxy) serveEncryptedFile(w http.ResponseWriter, r *http.Request, id string) {
	c := p.GetClient()
	if c == nil || c.BaseURL == "" {
		p.writeError(w, http.StatusServiceUnavailable, "surge not connected")
		return
	}

	password := r.URL.Query().Get("password")
	if password == "" {
		p.writeError(w, http.StatusBadRequest, "missing password parameter")
		return
	}

	status, err := c.GetStatus(r.Context(), id)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			p.writeError(w, http.StatusBadGateway, "surge request failed")
			return
		}
		entries, herr := c.History(r.Context())
		if herr != nil {
			p.writeError(w, http.StatusNotFound, "download not found")
			return
		}
		for _, e := range entries {
			if e.ID == id {
				p.encryptAndStream(w, r, e.DestPath, e.Filename, password)
				return
			}
		}
		p.writeError(w, http.StatusNotFound, "download not found")
		return
	}
	p.encryptAndStream(w, r, status.DestPath, status.Filename, password)
}

func (p *Proxy) encryptAndStream(w http.ResponseWriter, r *http.Request, destPath, filename, password string) {
	if destPath == "" {
		p.writeError(w, http.StatusNotFound, "file path not available")
		return
	}

	if filename == "" {
		filename = filepath.Base(destPath)
	}

	f, err := os.Open(filepath.Clean(destPath))
	if err != nil {
		p.writeError(w, http.StatusNotFound, "file not found on disk")
		return
	}
	defer f.Close()

	encKey := sha256.Sum256([]byte("enc" + password))

	block, err := aes.NewCipher(encKey[:])
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "encryption error")
		return
	}

	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		p.writeError(w, http.StatusInternalServerError, "encryption error")
		return
	}

	ctr := cipher.NewCTR(block, nonce)

	encFilename := filename + ".enc"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.QueryEscape(encFilename))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	flusher, canFlush := w.(http.Flusher)

	w.WriteHeader(http.StatusOK)

	w.Write([]byte("SENC"))
	w.Write([]byte{0x02})
	w.Write(nonce)

	nameLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(nameLen, uint32(len(filename)))
	nameHeader := append(nameLen, []byte(filename)...)

	encHeader := make([]byte, len(nameHeader))
	ctr.XORKeyStream(encHeader, nameHeader)
	w.Write(encHeader)

	buf := make([]byte, 64*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			ctr.XORKeyStream(buf[:n], buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
}
