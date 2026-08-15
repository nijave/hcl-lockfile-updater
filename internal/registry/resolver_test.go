package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverOpenTofuPackages(t *testing.T) {
	pkg, _ := os.ReadFile("../../testdata/registry/download-packages.json")
	var metaHits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&metaHits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(pkg)
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	r := NewResolver(NewClient(srv.Client()))
	got, err := r.Hashes(context.Background(), addr, "6.0.0", []string{"linux_amd64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 { // one zh + one h1 for linux_amd64
		t.Fatalf("got %d hashes, want 2: %v", len(got), got)
	}
	// second call must hit the cache, not the server
	_, _ = r.Hashes(context.Background(), addr, "6.0.0", []string{"darwin_arm64"})
	if atomic.LoadInt32(&metaHits) != 1 {
		t.Errorf("expected 1 metadata fetch, got %d", metaHits)
	}
}

// Concurrent lookups of the same key must fetch from the registry exactly
// once, not once per goroutine that races past the cache check.
func TestResolverConcurrentSameKeyFetchesOnce(t *testing.T) {
	pkg, _ := os.ReadFile("../../testdata/registry/download-packages.json")
	var metaHits int32
	gate := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&metaHits, 1)
		<-gate // hold responses until every goroutine is parked on the miss
		w.Header().Set("Content-Type", "application/json")
		w.Write(pkg)
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	r := NewResolver(NewClient(srv.Client()))

	const n = 4
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Hashes(context.Background(), addr, "6.0.0", []string{"linux_amd64"}); err != nil {
				t.Errorf("Hashes: %v", err)
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(gate)
	wg.Wait()
	if got := atomic.LoadInt32(&metaHits); got != 1 {
		t.Errorf("metadata fetches = %d, want 1", got)
	}
}

// A platform absent from the packages response must error, not silently
// produce a hash list that is short that platform's entries.
func TestResolverPackagesMissingPlatformErrors(t *testing.T) {
	pkg, _ := os.ReadFile("../../testdata/registry/download-packages.json")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(pkg)
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	r := NewResolver(NewClient(srv.Client()))
	_, err := r.Hashes(context.Background(), addr, "6.0.0", []string{"linux_amd64", "solaris_amd64"})
	if err == nil {
		t.Fatal("expected error for platform missing from packages response")
	}
	if !strings.Contains(err.Error(), "solaris_amd64") {
		t.Errorf("error should name the missing platform: %v", err)
	}
}

// A platform with no line in the SHASUMS document must error for the same
// reason.
func TestResolverSHASUMSMissingPlatformErrors(t *testing.T) {
	plain, _ := os.ReadFile("../../testdata/registry/download-plain.json")
	shasums, _ := os.ReadFile("../../testdata/registry/shasums.txt")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/providers/hashicorp/aws/6.0.0/download/linux/amd64":
			w.Header().Set("Content-Type", "application/json")
			w.Write(plain)
		case "/SHA256SUMS":
			w.Write(shasums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	r := NewResolver(NewClient(srv.Client()))
	_, err := r.Hashes(context.Background(), addr, "6.0.0", []string{"linux_amd64", "plan9_amd64"})
	if err == nil {
		t.Fatal("expected error for platform missing from SHASUMS document")
	}
	if !strings.Contains(err.Error(), "plan9_amd64") {
		t.Errorf("error should name the missing platform: %v", err)
	}
}

func TestResolverSHASUMS(t *testing.T) {
	plain, _ := os.ReadFile("../../testdata/registry/download-plain.json")
	shasums, _ := os.ReadFile("../../testdata/registry/shasums.txt")
	// rewrite the plain fixture's shasums_url to point at /shasums on this server
	var shasumsHits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/providers/hashicorp/aws/6.0.0/download/linux/amd64":
			w.Header().Set("Content-Type", "application/json")
			w.Write(plain)
		case "/SHA256SUMS":
			atomic.AddInt32(&shasumsHits, 1)
			w.Write(shasums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	r := NewResolver(NewClient(srv.Client()))
	got, err := r.Hashes(context.Background(), addr, "6.0.0", []string{"linux_amd64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"zh:aaaa"}
	if len(got) != 1 || got[0] != "zh:aaaa" {
		t.Errorf("got %v, want %v", got, want)
	}
}
