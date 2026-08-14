package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestListVersions(t *testing.T) {
	body, _ := os.ReadFile("../../testdata/registry/versions.json")
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
	want := []string{"5.0.0", "6.0.0"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
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
