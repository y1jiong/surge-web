package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"surge-web/internal/handler"
	"surge-web/internal/surge"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	var (
		port          int
		surgeHost     string
		surgePort     int
		surgeToken    string
		downloadDir   string
	)
	flag.IntVar(&port, "port", 8080, "Web UI listen port")
	flag.StringVar(&surgeHost, "surge-host", "", "Surge server address (default: auto-detect)")
	flag.IntVar(&surgePort, "surge-port", 0, "Surge server port (default: auto-detect)")
	flag.StringVar(&surgeToken, "token", "", "Surge API token (default: auto-detect)")
	flag.StringVar(&downloadDir, "dl-dir", "", "Download output directory (default: Surge's default)")
	flag.Parse()

	logger := log.New(os.Stderr, "[surge-web] ", log.LstdFlags)

	client := createClient(surgeHost, surgePort, surgeToken, logger)

	if client.BaseURL != "" {
		if err := client.Health(); err != nil {
			logger.Printf("Warning: Surge at %s is not reachable: %v", client.BaseURL, err)
		} else {
			logger.Printf("Connected to Surge at %s", client.BaseURL)
		}
	} else {
		logger.Printf("Warning: No Surge instance found. Start Surge with 'surge server', then reload this page.")
	}

	proxy := handler.NewProxy(client, logger)

	mux := proxy.ServeMux()

	webFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		logger.Fatalf("Failed to load web files: %v", err)
	}
	fileServer := http.FileServer(http.FS(webFS))
	mux.Handle("/", fileServer)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: withCORS(mux),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Fatalf("Failed to listen on :%d: %v", port, err)
	}

	logger.Printf("Web UI available at http://localhost:%d", port)
	if client.Token != "" {
		logger.Printf("API token: %s", maskToken(client.Token))
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Printf("Received %v, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Server error: %v", err)
	}
	logger.Println("Server stopped")
}

func createClient(host string, port int, token string, logger *log.Logger) *surge.Client {
	if host != "" || port > 0 {
		if host == "" {
			host = "127.0.0.1"
		}
		if port == 0 {
			port = 1700
		}
		baseURL := fmt.Sprintf("http://%s:%d", host, port)
		logger.Printf("Using explicit Surge address: %s", baseURL)
		return surge.NewClient(baseURL, token)
	}

	logger.Println("Auto-discovering Surge...")
	client, err := surge.NewClientFromDiscovery()
	if err != nil {
		logger.Printf("Auto-discovery failed: %v", err)
		return &surge.Client{}
	}
	return client
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + "..." + t[len(t)-4:]
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS, PUT, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
