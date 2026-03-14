package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
)

// 1. Core Components: The Service Model

// Service represents a single upstream server.
type Service struct {
	URL     *url.URL
	Latency time.Duration
	IsAlive bool
}

// 2. Thread-Safe Registry

// Registry manages the state of all upstream services.
// Uses an RWMutex to ensure the proxy can continuously read service status
type Registry struct {
	mu       sync.RWMutex
	services []Service
}

func NewRegistry(targetURLs []string) *Registry {
	var services []Service
	for _, rawURL := range targetURLs {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			log.Fatalf("Invalid URL %s: %v", rawURL, err)
		}
		services = append(services, Service{
			URL:     parsed,
			IsAlive: false, // Default to false until first scan
		})
	}
	return &Registry{services: services}
}

// 3. The Concurrent Scanner & Dashboard

// Scan iterates through all services, launching a Goroutine for each.
func (r *Registry) Scan() {
	r.mu.RLock()
	count := len(r.services)
	r.mu.RUnlock()

	// Track the concurrent health check routines to ensure all finish before rendering the dashboard.
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)

		// Execute the health check concurrently.
		go func(index int) {
			// Ensure we signal completion to the WaitGroup when this check finishes or fails.
			defer wg.Done()

			// Safely read the URL
			r.mu.RLock()
			targetURL := r.services[index].URL
			r.mu.RUnlock()

			// Perform Health Check
			start := time.Now()
			client := http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(targetURL.String())
			
			latency := time.Since(start)
			isAlive := err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400

			if resp != nil {
				resp.Body.Close()
			}

			// Safely write results back to the registry
			r.mu.Lock()
			r.services[index].Latency = latency
			r.services[index].IsAlive = isAlive
			r.mu.Unlock() // We don't defer here to keep the lock duration as short as possible
		}(i)
	}

	// Block until all wg.Done() calls are made
	wg.Wait()
	r.printDashboard()
}

// printDashboard utilizes text/tabwriter to render a clean CLI TUI
func (r *Registry) printDashboard() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Clear screen using ANSI escape codes for a real "Dashboard" feel
	fmt.Print("\033[H\033[2J")
	fmt.Println("=== Distributed Health Monitor & Smart Proxy ===")
	fmt.Printf("Last Scan: %s\n\n", time.Now().Format(time.RFC3339))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "TARGET URL\tSTATUS\tLATENCY\t")
	
	for _, s := range r.services {
		status := "❌ DOWN"
		if s.IsAlive {
			status = "✅ UP"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t\n", s.URL.String(), status, s.Latency.Round(time.Millisecond))
	}
	w.Flush()
	fmt.Println("\nRouting traffic to the fastest healthy service at: http://localhost:8080")
}

// GetFastestHealthy returns the URL of the fastest service.
func (r *Registry) GetFastestHealthy() (*url.URL, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var fastest *Service
	for i := range r.services {
		s := &r.services[i]
		if s.IsAlive {
			if fastest == nil || s.Latency < fastest.Latency {
				fastest = s
			}
		}
	}

	if fastest == nil {
		return nil, errors.New("no healthy services available")
	}
	return fastest.URL, nil
}

// 4. The Smart Reverse Proxy

func NewSmartProxy(registry *Registry) *httputil.ReverseProxy {
	// The ReverseProxy uses a Director to intercept and modify requests in-flight.
	// We dynamically rewrite the request URL here to route incoming traffic
	// to the optimal upstream server based on the latest health scan.
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			targetURL, err := registry.GetFastestHealthy()
			if err != nil {
				// If no hosts are healthy, we set an invalid host to intentionally 
				// trigger the ErrorHandler below.
				req.URL.Host = "offline"
				return
			}

			// Print out exactly what is being rerouted and to where
			fmt.Printf("[PROXY] Rerouting request for '%s' -> %s\n", req.URL.Path, targetURL.String())

			// Rewrite the request to target the fastest backend
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host // Overwrite the host header
			
			// We intentionally leave req.URL.Path alone so the user's path 
			// (e.g., /api/data) proxies directly to the target intact.
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("502 Bad Gateway: No healthy upstream servers available.\n"))
		},
	}
}

// 5. Orchestration & Graceful Shutdown

func main() {
	// 1. Setup Signal handling for Graceful Shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 2. Initialize Registry with some test targets 
	// (Using reliable public endpoints for immediate testing)
	registry := NewRegistry([]string{
		"https://cloudflare-dns.com",
		"https://dns.google",
		"https://httpbin.org",
	})

	// 3. Initial Scan so the proxy knows where to route instantly
	registry.Scan()

	// 4. Start HTTP Server with our Smart Proxy
	proxy := NewSmartProxy(registry)
	server := &http.Server{
		Addr:    ":8080",
		Handler: proxy,
	}

	// Run server in a background Goroutine so it doesn't block orchestration
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Proxy server failed: %v", err)
		}
	}()

	// 5. Orchestration Loop (Ticker + Select)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Main orchestration loop.
	// This blocks and waits to process background timer events (for rescanning) or system termination signals (for graceful shutdown).
	for {
		select {
		case <-ticker.C:
			registry.Scan()
			
		case <-ctx.Done():
			// OS Interruption signal received (Ctrl+C / SIGTERM)
			fmt.Println("\n[!] Shutting down proxy server gracefully...")
			
			// Give active proxy connections 5 seconds to finish before hard killing
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Fatalf("Server forced to shutdown: %v", err)
			}
			
			fmt.Println("Server exited cleanly.")
			return
		}
	}
}