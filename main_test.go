package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newDelayedServer returns an httptest server (127.0.0.1 only) that sleeps
// for delay before responding 200 with the given body on any path.
func newDelayedServer(delay time.Duration, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
}

func TestGetFastestHealthyPicksLowestLatency(t *testing.T) {
	fast := newDelayedServer(5*time.Millisecond, "fast")
	defer fast.Close()
	mid := newDelayedServer(50*time.Millisecond, "mid")
	defer mid.Close()
	slow := newDelayedServer(200*time.Millisecond, "slow")
	defer slow.Close()

	registry := NewRegistry([]string{fast.URL, mid.URL, slow.URL})
	registry.Scan()

	got, err := registry.GetFastestHealthy()
	if err != nil {
		t.Fatalf("GetFastestHealthy returned unexpected error: %v", err)
	}
	if got.String() != fast.URL {
		t.Fatalf("expected fastest %q, got %q", fast.URL, got.String())
	}
}

func TestFailoverSkipsDownBackend(t *testing.T) {
	fast := newDelayedServer(5*time.Millisecond, "fast")
	mid := newDelayedServer(50*time.Millisecond, "mid")
	defer mid.Close()
	slow := newDelayedServer(200*time.Millisecond, "slow")
	defer slow.Close()

	registry := NewRegistry([]string{fast.URL, mid.URL, slow.URL})
	registry.Scan()

	got, err := registry.GetFastestHealthy()
	if err != nil {
		t.Fatalf("initial GetFastestHealthy returned unexpected error: %v", err)
	}
	if got.String() != fast.URL {
		t.Fatalf("expected initial fastest %q, got %q", fast.URL, got.String())
	}

	// Take the fastest backend down and rescan: 50ms server should win.
	fast.Close()
	registry.Scan()

	got, err = registry.GetFastestHealthy()
	if err != nil {
		t.Fatalf("post-failover GetFastestHealthy returned unexpected error: %v", err)
	}
	if got.String() != mid.URL {
		t.Fatalf("expected failover fastest %q, got %q", mid.URL, got.String())
	}
}

func TestNoHealthyReturnsError(t *testing.T) {
	s1 := newDelayedServer(5*time.Millisecond, "s1")
	s2 := newDelayedServer(50*time.Millisecond, "s2")
	s3 := newDelayedServer(200*time.Millisecond, "s3")

	registry := NewRegistry([]string{s1.URL, s2.URL, s3.URL})

	s1.Close()
	s2.Close()
	s3.Close()

	registry.Scan()

	if _, err := registry.GetFastestHealthy(); err == nil {
		t.Fatal("expected error when no backends are healthy, got nil")
	}
}

func TestProxyRoutesToFastest(t *testing.T) {
	fast := newDelayedServer(5*time.Millisecond, "fast-backend")
	defer fast.Close()
	slow := newDelayedServer(200*time.Millisecond, "slow-backend")
	defer slow.Close()

	registry := NewRegistry([]string{fast.URL, slow.URL})
	registry.Scan()

	proxy := NewSmartProxy(registry)

	req := httptest.NewRequest(http.MethodGet, "/get", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "fast-backend" {
		t.Fatalf("expected fastest backend body %q, got %q", "fast-backend", rec.Body.String())
	}
}

func TestProxyAllDownReturns502(t *testing.T) {
	s1 := newDelayedServer(5*time.Millisecond, "s1")
	s2 := newDelayedServer(50*time.Millisecond, "s2")
	urls := []string{s1.URL, s2.URL}
	s1.Close()
	s2.Close()

	registry := NewRegistry(urls)
	registry.Scan()

	proxy := NewSmartProxy(registry)

	req := httptest.NewRequest(http.MethodGet, "/get", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No healthy upstream") {
		t.Fatalf("expected body to contain %q, got %q", "No healthy upstream", rec.Body.String())
	}
}
