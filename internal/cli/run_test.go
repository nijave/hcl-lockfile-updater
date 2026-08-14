package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nijave/terragrunt-providers-pin/internal/registry"
)

func newRegServer(t *testing.T, versions, pkg, shasums []byte, metaHits *int32) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/providers/hashicorp/aws/versions":
			w.Header().Set("Content-Type", "application/json")
			w.Write(versions)
		case "/v1/providers/hashicorp/aws/6.0.0/download/linux/amd64":
			atomic.AddInt32(metaHits, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Write(pkg)
		case "/SHA256SUMS":
			w.Write(shasums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunLookupWritesLockFile(t *testing.T) {
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"filename":"x.zip","shasums_url":"https://releases.example.com/SHA256SUMS","packages":{"linux_amd64":{"hashes":["zh:aaa","h1:bbb="]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	os.WriteFile(lockPath, []byte(`provider "`+addr.String()+`" {
  version     = "5.0.0"
  constraints = "~> 5.0"
  hashes = ["h1:old="]
}
`), 0o644)

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"linux_amd64"}, LockFiles: []string{lockPath},
	}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: &bytes.Buffer{}}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(lockPath)
	s := string(got)
	if !strings.Contains(s, `"6.0.0"`) || strings.Contains(s, `"5.0.0"`) {
		t.Errorf("version not updated:\n%s", s)
	}
	if !strings.Contains(s, `constraints = "~> 5.0"`) {
		t.Errorf("constraints not preserved:\n%s", s)
	}
	if !strings.Contains(s, "zh:aaa") {
		t.Errorf("hash missing:\n%s", s)
	}
}

func TestRunLookupCacheHitsOnceAcrossFiles(t *testing.T) {
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"shasums_url":"https://releases.example.com/SHA256SUMS","packages":{"linux_amd64":{"hashes":["zh:aaa"]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.lock.hcl")
	p2 := filepath.Join(dir, "b.lock.hcl")

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"linux_amd64"}, LockFiles: []string{p1, p2},
	}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: &bytes.Buffer{}}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Run resolves hashes once before looping over files, so one metadata fetch.
	if atomic.LoadInt32(&metaHits) != 1 {
		t.Errorf("expected 1 metadata fetch, got %d", metaHits)
	}
}

func TestRunPrintBlock(t *testing.T) {
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"packages":{"linux_amd64":{"hashes":["zh:aaa","h1:bbb="]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	cfg := Config{Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0", Platforms: []string{"linux_amd64"}, PrintBlock: true}
	out := &bytes.Buffer{}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: out}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	printed := out.String()
	if !strings.Contains(printed, `provider "`) || !strings.Contains(printed, "zh:aaa") {
		t.Errorf("printed block wrong:\n%s", printed)
	}
	// Fix #3: output must end with exactly one newline.
	if !strings.HasSuffix(printed, "\n") {
		t.Errorf("printed block does not end with newline")
	}
	if strings.HasSuffix(printed, "\n\n") {
		t.Errorf("printed block has double trailing newline")
	}
}

func TestRunPrintBlockLeavesLockFileUnchanged(t *testing.T) {
	orig := []byte(`provider "registry.opentofu.org/hashicorp/aws" {
  version     = "5.0.0"
  constraints = "~> 5.0"
  hashes = ["h1:old="]
}
`)
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"packages":{"linux_amd64":{"hashes":["zh:aaa","h1:bbb="]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	if err := os.WriteFile(lockPath, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"linux_amd64"}, PrintBlock: true,
		LockFiles: []string{lockPath},
	}
	out := &bytes.Buffer{}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: out}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PrintBlock short-circuits before the write loop, so stdout should have content
	if out.Len() == 0 {
		t.Fatal("expected non-empty stdout from --print-block")
	}
	// The lock file on disk must be byte-for-byte unchanged
	got, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Errorf("lock file was modified:\n%s", string(got))
	}
}

func TestRunVerbatimConstraintsOverride(t *testing.T) {
	dir := t.TempDir()
	blockPath := filepath.Join(dir, "block.hcl")
	os.WriteFile(blockPath, []byte(`provider "registry.opentofu.org/cloudflare/cloudflare" {
  version     = "5.22.0"
  constraints = "5.22.0"
  hashes = ["zh:aaaa"]
}
`), 0o644)
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	os.WriteFile(lockPath, []byte(`provider "registry.opentofu.org/cloudflare/cloudflare" {
  version = "5.20.0"
  hashes  = ["zh:old"]
}
`), 0o644)

	cfg := Config{Mode: ModeVerbatim, BlockFile: blockPath, Constraints: "5.99.0", LockFiles: []string{lockPath}}
	deps := Deps{Stdout: &bytes.Buffer{}}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(lockPath)
	s := string(got)
	if !strings.Contains(s, `constraints = "5.99.0"`) {
		t.Errorf("override constraints not applied:\n%s", s)
	}
	if strings.Contains(s, `constraints = "5.22.0"`) {
		t.Errorf("block's original constraints should have been overridden:\n%s", s)
	}
}

func TestRunVerbatim(t *testing.T) {
	dir := t.TempDir()
	blockPath := filepath.Join(dir, "block.hcl")
	os.WriteFile(blockPath, []byte(`provider "registry.opentofu.org/cloudflare/cloudflare" {
  version     = "5.22.0"
  constraints = "5.22.0"
  hashes = ["zh:aaaa"]
}
`), 0o644)
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	os.WriteFile(lockPath, []byte(`provider "registry.opentofu.org/cloudflare/cloudflare" {
  version = "5.20.0"
  hashes  = ["zh:old"]
}
`), 0o644)

	cfg := Config{Mode: ModeVerbatim, BlockFile: blockPath, LockFiles: []string{lockPath}}
	deps := Deps{Stdout: &bytes.Buffer{}}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(lockPath)
	s := string(got)
	if !strings.Contains(s, `"5.22.0"`) || strings.Contains(s, `"5.20.0"`) {
		t.Errorf("version not updated:\n%s", s)
	}
	if !strings.Contains(s, `constraints = "5.22.0"`) {
		t.Errorf("constraints not applied:\n%s", s)
	}
}

// --- New tests for review findings ---

func TestRunLookupCreatesMissingLockFile(t *testing.T) {
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"filename":"x.zip","packages":{"linux_amd64":{"hashes":["zh:aaa","h1:bbb="]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	// File does not exist yet.

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"linux_amd64"}, LockFiles: []string{lockPath},
	}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: &bytes.Buffer{}}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("lock file was not created: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, `"6.0.0"`) {
		t.Errorf("version missing from created lock file:\n%s", s)
	}
	if !strings.Contains(s, "zh:aaa") {
		t.Errorf("hash missing from created lock file:\n%s", s)
	}
}

func TestRunLookupReadErrorPropagates(t *testing.T) {
	var metaHits int32
	srv := newRegServer(t,
		[]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`),
		[]byte(`{"filename":"x.zip","packages":{"linux_amd64":{"hashes":["zh:aaa"]}}}`),
		nil, &metaHits)
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "subdir/.terraform.lock.hcl")
	subdir := filepath.Join(dir, "subdir")
	os.Mkdir(subdir, 0o000) // make unreadable

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"linux_amd64"}, LockFiles: []string{lockPath},
	}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: &bytes.Buffer{}}
	err := Run(context.Background(), cfg, deps)
	// The ReadFile should fail with a permission error.
	if err == nil {
		t.Fatal("expected error for unreadable lock file path")
	}
	// Clean up: restore permissions so temp dir can be removed.
	os.Chmod(subdir, 0o755)
}

func TestRunLookupPlatformNotAvailable(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/providers/hashicorp/aws/versions":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"versions":[{"version":"6.0.0","platforms":[{"os":"linux","arch":"amd64"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	addr := registry.ProviderAddr{Host: srv.Listener.Addr().String(), Namespace: "hashicorp", Type: "aws"}
	client := registry.NewClient(srv.Client())

	cfg := Config{
		Mode: ModeLookup, ProviderRaw: addr.String(), Version: "6.0.0",
		Platforms: []string{"darwin_arm64"}, PrintBlock: true,
	}
	out := &bytes.Buffer{}
	deps := Deps{Lister: client, Resolver: registry.NewResolver(client), Stdout: out}
	err := Run(context.Background(), cfg, deps)
	if err == nil {
		t.Fatal("expected error for unavailable platform")
	}
	if !strings.Contains(err.Error(), `platform "darwin_arm64" not available`) {
		t.Errorf("error missing platform detail: %v", err)
	}
	if !strings.Contains(err.Error(), "linux_amd64") {
		t.Errorf("error missing available platforms: %v", err)
	}
}

func TestAtomicWritePreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".terraform.lock.hcl")
	// Create an existing file with 0644.
	os.WriteFile(lockPath, []byte("old\n"), 0o644)

	data := []byte("new\n")
	if err := atomicWrite(lockPath, data); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode after write = %o, want 0644", info.Mode().Perm())
	}
	got, _ := os.ReadFile(lockPath)
	if string(got) != "new\n" {
		t.Errorf("content wrong: %q", string(got))
	}
}

func TestRunVerbatimPrintBlock(t *testing.T) {
	dir := t.TempDir()
	blockPath := filepath.Join(dir, "block.hcl")
	os.WriteFile(blockPath, []byte(`provider "registry.opentofu.org/cloudflare/cloudflare" {
  version     = "5.22.0"
  constraints = "5.22.0"
  hashes = ["zh:aaaa"]
}
`), 0o644)

	cfg := Config{Mode: ModeVerbatim, BlockFile: blockPath, PrintBlock: true}
	out := &bytes.Buffer{}
	deps := Deps{Stdout: out}
	if err := Run(context.Background(), cfg, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	printed := out.String()
	if !strings.Contains(printed, `provider "`) || !strings.Contains(printed, "zh:aaaa") {
		t.Errorf("verbatim --print-block wrong:\n%s", printed)
	}
	// Must end with exactly one newline.
	if !strings.HasSuffix(printed, "\n") || strings.HasSuffix(printed, "\n\n") {
		t.Errorf("verbatim --print-block trailing newline wrong")
	}
}
