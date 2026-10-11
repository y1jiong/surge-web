package handler

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge-web/internal/surge"
)

// newTestProxy wires a Proxy to a fake Surge API served by httptest.
func newTestProxy(t *testing.T, surgeHandler http.HandlerFunc) *Proxy {
	t.Helper()

	srv := httptest.NewServer(surgeHandler)
	t.Cleanup(srv.Close)

	p := NewProxy(log.New(io.Discard, "", 0))
	p.SetClient(surge.NewClient(srv.URL, "test-token"))
	return p
}

func writeFixture(t *testing.T, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

type hashResponse struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MD5      string `json:"md5"`
	SHA256   string `json:"sha256"`
}

func requestHash(t *testing.T, p *Proxy, id string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	p.ServeMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/file/"+id+"/hash", nil))
	return rec
}

func TestHashFileReturnsMD5AndSHA256(t *testing.T) {
	content := []byte("surge-web hash test payload\n")
	path := writeFixture(t, "payload.bin", content)

	p := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		_ = json.MarshalWrite(w, surge.DownloadStatus{
			ID:       "abc",
			Filename: "payload.bin",
			DestPath: path,
			Status:   "completed",
		})
	})

	rec := requestHash(t, p, "abc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got hashResponse
	if err := json.UnmarshalRead(rec.Body, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	md5Want := md5.Sum(content)
	shaWant := sha256.Sum256(content)
	if got.MD5 != hex.EncodeToString(md5Want[:]) {
		t.Errorf("md5 = %q, want %q", got.MD5, hex.EncodeToString(md5Want[:]))
	}
	if got.SHA256 != hex.EncodeToString(shaWant[:]) {
		t.Errorf("sha256 = %q, want %q", got.SHA256, hex.EncodeToString(shaWant[:]))
	}
	if got.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", got.Size, len(content))
	}
	if got.ID != "abc" || got.Filename != "payload.bin" {
		t.Errorf("id/filename = %q/%q, want abc/payload.bin", got.ID, got.Filename)
	}
}

func TestHashFileFallsBackToHistory(t *testing.T) {
	content := []byte("completed downloads leave the live status endpoint\n")
	path := writeFixture(t, "archived.bin", content)

	p := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			http.NotFound(w, r)
		case "/history":
			_ = json.MarshalWrite(w, []surge.DownloadEntry{{
				ID:       "archived",
				Filename: "archived.bin",
				DestPath: path,
				Status:   "completed",
			}})
		default:
			http.NotFound(w, r)
		}
	})

	rec := requestHash(t, p, "archived")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got hashResponse
	if err := json.UnmarshalRead(rec.Body, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	shaWant := sha256.Sum256(content)
	if got.SHA256 != hex.EncodeToString(shaWant[:]) {
		t.Errorf("sha256 = %q, want %q", got.SHA256, hex.EncodeToString(shaWant[:]))
	}
}

func TestHashFileRejectsIncompleteDownload(t *testing.T) {
	path := writeFixture(t, "partial.bin.surge", []byte("half a file"))

	p := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		_ = json.MarshalWrite(w, surge.DownloadStatus{
			ID:       "partial",
			Filename: "partial.bin",
			DestPath: path,
			Status:   "downloading",
		})
	})

	rec := requestHash(t, p, "partial")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestContextReaderStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := contextReader{ctx: ctx, r: strings.NewReader("payload")}

	buf := make([]byte, 4)
	if _, err := src.Read(buf); err != nil {
		t.Fatalf("read before cancel: %v", err)
	}

	cancel()
	if _, err := src.Read(buf); !errors.Is(err, context.Canceled) {
		t.Fatalf("read after cancel = %v, want %v", err, context.Canceled)
	}
}

func TestHashFileMissingOnDisk(t *testing.T) {
	p := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		_ = json.MarshalWrite(w, surge.DownloadStatus{
			ID:       "gone",
			Filename: "gone.bin",
			DestPath: filepath.Join(t.TempDir(), "gone.bin"),
			Status:   "completed",
		})
	})

	rec := requestHash(t, p, "gone")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
