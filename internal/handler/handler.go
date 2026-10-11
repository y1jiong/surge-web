package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
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

// maxConcurrentHashes caps how many downloads are hashed at the same time.
// Hashing reads a whole file, so a handful of large downloads could otherwise
// saturate the disk.
const maxConcurrentHashes = 2

type Proxy struct {
	mu      sync.RWMutex
	client  *surge.Client
	Logger  *log.Logger
	hashSem chan struct{}
}

func NewProxy(logger *log.Logger) *Proxy {
	return &Proxy{
		Logger:  logger,
		hashSem: make(chan struct{}, maxConcurrentHashes),
	}
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
	_ = json.MarshalWrite(w, data)
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
		if len(parts) > 1 {
			switch parts[1] {
			case "encrypt":
				p.serveEncryptedFile(w, r, id)
				return
			case "hash":
				p.hashFile(w, r, id)
				return
			}
		}
		p.serveFile(w, r, id)
	})

	return mux
}

// fileRef is a download's on-disk location and status, resolved from its id.
type fileRef struct {
	DestPath string
	Filename string
	Status   string
}

// resolveFileRef maps a download id to its fileRef. It asks the status endpoint
// first and, when the daemon reports the id as not found, retries against
// history so entries listed only there stay reachable. The returned int is the
// HTTP status to report when err is non-nil.
func (p *Proxy) resolveFileRef(ctx context.Context, id string) (*fileRef, int, error) {
	c := p.GetClient()
	if c == nil || c.BaseURL == "" {
		return nil, http.StatusServiceUnavailable, errors.New("surge not connected")
	}

	status, err := c.GetStatus(ctx, id)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return nil, http.StatusBadGateway, errors.New("surge request failed")
		}
		entries, herr := c.History(ctx)
		if herr != nil {
			return nil, http.StatusNotFound, errors.New("download not found")
		}
		for _, e := range entries {
			if e.ID == id {
				return &fileRef{DestPath: e.DestPath, Filename: e.Filename, Status: e.Status}, 0, nil
			}
		}
		return nil, http.StatusNotFound, errors.New("download not found")
	}
	return &fileRef{DestPath: status.DestPath, Filename: status.Filename, Status: status.Status}, 0, nil
}

func (p *Proxy) serveFile(w http.ResponseWriter, r *http.Request, id string) {
	ref, code, err := p.resolveFileRef(r.Context(), id)
	if err != nil {
		p.writeError(w, code, err.Error())
		return
	}
	p.streamFile(w, r, ref.DestPath, ref.Filename)
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
	ref, code, err := p.resolveFileRef(r.Context(), id)
	if err != nil {
		p.writeError(w, code, err.Error())
		return
	}

	password := r.URL.Query().Get("password")
	if password == "" {
		p.writeError(w, http.StatusBadRequest, "missing password parameter")
		return
	}

	p.encryptAndStream(w, r, ref.DestPath, ref.Filename, password)
}

// contextReader fails reads once ctx is done, so a long file copy stops at the
// next chunk instead of running to completion.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// hashFile reads the file behind a completed download and reports its MD5 and
// SHA-256 digests. Both are computed in a single pass over the file, and the
// copy stops if the request is canceled.
func (p *Proxy) hashFile(w http.ResponseWriter, r *http.Request, id string) {
	ref, code, err := p.resolveFileRef(r.Context(), id)
	if err != nil {
		p.writeError(w, code, err.Error())
		return
	}

	if ref.Status != "completed" {
		p.writeError(w, http.StatusConflict, "only completed downloads can be hashed")
		return
	}
	if ref.DestPath == "" {
		p.writeError(w, http.StatusNotFound, "file path not available")
		return
	}

	select {
	case p.hashSem <- struct{}{}:
		defer func() { <-p.hashSem }()
	case <-r.Context().Done():
		// The request was canceled while waiting for a free slot.
		return
	}

	f, err := os.Open(filepath.Clean(ref.DestPath))
	if err != nil {
		p.writeError(w, http.StatusNotFound, "file not found on disk")
		return
	}
	defer f.Close()

	md5Sum := md5.New()
	sha256Sum := sha256.New()

	src := contextReader{ctx: r.Context(), r: f}
	size, err := io.Copy(io.MultiWriter(md5Sum, sha256Sum), src)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		p.Logger.Printf("hash %s: read %s: %v", id, ref.DestPath, err)
		p.writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	filename := ref.Filename
	if filename == "" {
		filename = filepath.Base(ref.DestPath)
	}

	p.writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"filename": filename,
		"size":     size,
		"md5":      hex.EncodeToString(md5Sum.Sum(nil)),
		"sha256":   hex.EncodeToString(sha256Sum.Sum(nil)),
	})
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
