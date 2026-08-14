package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestListVersions(t *testing.T) {
	body, err := os.ReadFile("../../testdata/registry/versions.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/providers/hashicorp/aws/versions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	c := NewClient(srv.Client())
	got, err := c.ListVersions(context.Background(), addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Version{
		{Version: "5.0.0", Platforms: []Platform{{OS: "linux", Arch: "amd64"}}},
		{Version: "6.0.0", Platforms: []Platform{{OS: "linux", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Verify the Versions helper extracts just the strings.
	strs := Versions(got)
	if !reflect.DeepEqual(strs, []string{"5.0.0", "6.0.0"}) {
		t.Fatalf("Versions() = %v, want [5.0.0 6.0.0]", strs)
	}
}

func TestListVersionsHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	c := NewClient(srv.Client())
	if _, err := c.ListVersions(context.Background(), addr); err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestPackageMeta(t *testing.T) {
	pkgBody, _ := os.ReadFile("../../testdata/registry/download-packages.json")
	plainBody, _ := os.ReadFile("../../testdata/registry/download-plain.json")

	route := func(b []byte) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
		})
	}

	t.Run("packages extension and relative url", func(t *testing.T) {
		srv := httptest.NewTLSServer(route(pkgBody))
		defer srv.Close()
		addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
		c := NewClient(srv.Client())
		meta, err := c.PackageMeta(context.Background(), addr, "6.0.0", "linux", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.ShasumsURL == "" || meta.ShasumsURL == "/v1/providers/hashicorp/aws/6.0.0/shasums" {
			t.Errorf("relative ShasumsURL not resolved: %q", meta.ShasumsURL)
		}
		if len(meta.Packages) != 2 {
			t.Errorf("expected 2 packages, got %d", len(meta.Packages))
		}
		h := meta.Packages["linux_amd64"].Hashes
		if len(h) != 2 || h[0] == "" {
			t.Errorf("unexpected linux_amd64 hashes: %v", h)
		}
	})

	t.Run("plain response has no packages", func(t *testing.T) {
		srv := httptest.NewTLSServer(route(plainBody))
		defer srv.Close()
		addr := ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
		c := NewClient(srv.Client())
		meta, err := c.PackageMeta(context.Background(), addr, "6.0.0", "linux", "amd64")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(meta.Packages) != 0 {
			t.Errorf("expected no packages, got %d", len(meta.Packages))
		}
		if meta.ShasumsURL == "" {
			t.Error("expected non-empty ShasumsURL")
		}
	})
}
