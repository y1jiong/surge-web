package main

import (
	"context"
	"embed"
	"errors"
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

	flag "github.com/spf13/pflag"

	"surge-web/internal/handler"
	"surge-web/internal/surge"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	var (
		port       int
		surgeHost  string
		surgePort  int
		surgeToken string
	)
	flag.IntVarP(&port, "port", "p", 8080, "listen port for the web UI")
	flag.StringVarP(&surgeHost, "surge-host", "H", "", "surge server address (default: auto-detect)")
	flag.IntVarP(&surgePort, "surge-port", "P", 0, "surge server port (default: auto-detect)")
	flag.StringVarP(&surgeToken, "token", "t", "", "surge API token (default: auto-detect)")
	flag.Parse()

	logger := log.New(os.Stderr, "[surge-web] ", log.LstdFlags)

	proxy := handler.NewProxy(logger)

	if surgeHost != "" || surgePort > 0 {
		tryConnect(proxy, surgeHost, surgePort, surgeToken, logger)
		startRetryLoop(proxy, logger, true, surgeHost, surgePort, surgeToken)
	} else {
		logger.Println("auto-discovering Surge...")
		tryConnect(proxy, surgeHost, surgePort, surgeToken, logger)
		startRetryLoop(proxy, logger, false, "", 0, "")
	}

	mux := proxy.ServeMux()

	webFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		logger.Fatalf("failed to load web files: %v", err)
	}
	fileServer := http.FileServer(http.FS(webFS))
	mux.Handle("/", fileServer)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: withCORS(mux),
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Fatalf("failed to listen on :%d: %v", port, err)
	}

	logger.Printf("web UI available at http://localhost:%d", port)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Printf("received %v, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server error: %v", err)
	}
	logger.Println("server stopped")
}

func tryConnect(proxy *handler.Proxy, host string, port int, token string, logger *log.Logger) {
	c, err := discoverAndConnect(host, port, token)
	if err != nil {
		logger.Printf("surge not available: %v", err)
		return
	}
	proxy.SetClient(c)
	logger.Printf("connected to Surge at %s", c.BaseURL)
	if c.Token != "" {
		logger.Printf("API token: %s", maskToken(c.Token))
	}
}

func discoverAndConnect(host string, port int, token string) (*surge.Client, error) {
	if host != "" || port > 0 {
		if host == "" {
			host = "127.0.0.1"
		}
		if port == 0 {
			port = 1700
		}
		baseURL := fmt.Sprintf("http://%s:%d", host, port)
		c := surge.NewClient(baseURL, token)
		if err := c.Health(); err != nil {
			return nil, fmt.Errorf("surge at %s not reachable: %w", baseURL, err)
		}
		return c, nil
	}

	c, err := surge.NewClientFromDiscovery()
	if err != nil {
		return nil, err
	}
	if err := c.Health(); err != nil {
		return nil, fmt.Errorf("surge at %s not reachable: %w", c.BaseURL, err)
	}
	return c, nil
}

func startRetryLoop(proxy *handler.Proxy, logger *log.Logger, explicit bool, host string, port int, token string) {
	go func() {
		backoff := 2 * time.Second
		const maxBackoff = 30 * time.Second
		for {
			c := proxy.GetClient()
			if c != nil && c.BaseURL != "" {
				if err := c.Health(); err != nil {
					logger.Printf("lost connection to Surge: %v", err)
					proxy.SetClient(nil)
				} else {
					backoff = 2 * time.Second
					time.Sleep(backoff)
					continue
				}
			}

			var discovered *surge.Client
			var err error
			if explicit {
				discovered, err = discoverAndConnect(host, port, token)
			} else {
				discovered, err = discoverAndConnect("", 0, "")
			}

			if err != nil {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				time.Sleep(backoff)
				continue
			}

			proxy.SetClient(discovered)
			logger.Printf("connected to Surge at %s", discovered.BaseURL)
			if discovered.Token != "" {
				logger.Printf("API token: %s", maskToken(discovered.Token))
			}
			backoff = 2 * time.Second
			time.Sleep(backoff)
		}
	}()
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
