# terragrunt-providers-pin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to work through this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that pins a provider (source address, version, hashes) into one or more `.terraform.lock.hcl` files, querying the provider registry for hashes, with a verbatim-block mode and a block-printing mode.

**Architecture:** Three internal packages — `registry` (HTTP client, version selection, hash resolution with in-memory cache), `lockfile` (HCL parse/render/merge via `hclwrite`, verbatim decode via `gohcl`), and `cli` (flag parsing + orchestration). `main.go` is a thin entry point. Both input modes reduce to one attribute-merge routine that preserves unspecified attributes.

**Tech Stack:** Go 1.25, `github.com/hashicorp/hcl/v2` (resolved to the OpenTofu fork via `replace`), `github.com/hashicorp/go-version`, `github.com/spf13/pflag`, `github.com/zclconf/go-cty` (transitive).

**Spec:** `docs/superpowers/specs/2026-08-13-terragrunt-providers-pin-design.md`

## Global Constraints

- Go 1.25+ toolchain. Module path: `github.com/nijave/terragrunt-providers-pin`.
- FOSS dependencies only: MPL-2.0, BSD-3-Clause, or MIT. No business or source-available licenses.
- Prefer the OpenTofu fork of HCL: consume `github.com/hashicorp/hcl/v2` through a `replace` directive pointing at `github.com/opentofu/hcl/v2`, keeping canonical import paths in code.
- Tests run with `go test ./...`; no live network in unit tests (use `net/http/httptest` + fixture files).
- Every task ends with a passing `go test` for the touched package (or a build/vet check for the setup task) and a commit. Use conventional-commit messages.

## File Structure

```
go.mod  go.sum  .gitignore  README.md  main.go
internal/
  cli/
    config.go        # Config struct + ParseArgs (pflag)
    config_test.go
    run.go           # Run(ctx, cfg, deps) orchestration
    run_test.go      # end-to-end with httptest
  registry/
    addr.go          # ProviderAddr + ParseProviderSource
    addr_test.go
    version.go       # SelectVersion
    version_test.go
    client.go        # Client: NewClient, ListVersions, PackageMeta, FetchSHASUMS
    client_test.go
    shasums.go       # ParseSHASUMSLines, ParseSHASUMS
    shasums_test.go
    resolver.go      # Resolver: Hashes + in-memory cache
    resolver_test.go
  lockfile/
    lockfile.go      # RenderProviderBlock, MergeProviderBlock
    lockfile_test.go
    verbatim.go      # DecodeVerbatimBlock
    verbatim_test.go
testdata/
  registry/*.json    # recorded registry responses
```

Dependency order (each task consumes earlier tasks' outputs):
1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12.

---

## Task 1: Module and dependency setup

**Files:**
- Create: `go.mod`, `go.sum`, `.gitignore`, `main.go`

**Interfaces:**
- Produces: a buildable module `github.com/nijave/terragrunt-providers-pin` with the three dependencies resolvable. Later tasks add packages under `internal/`.

- [ ] **Step 1: Initialize the module and stub main.go**

Run:
```bash
go mod init github.com/nijave/terragrunt-providers-pin
```

Create `main.go`:
```go
package main

func main() {}
```

Create `.gitignore`:
```
/terragrunt-providers-pin
/terragrunt-providers-pin.exe
*.out
*.test
```

- [ ] **Step 2: Add dependencies and the OpenTofu HCL replace**

Run:
```bash
go get github.com/hashicorp/hcl/v2@v2.20.1
go get github.com/hashicorp/go-version@latest
go get github.com/spf13/pflag@latest
```

Manually add this `replace` line to `go.mod` (under the `require` block, in its own section):
```
replace github.com/hashicorp/hcl/v2 => github.com/opentofu/hcl/v2 v2.20.2-0.20251021132045-587d123c2828
```

Then run:
```bash
go mod tidy
```

If `go mod tidy` fails because the pseudo-version is unavailable, look up the current fork version in `https://github.com/opentofu/opentofu/blob/main/go.mod` (search for `replace github.com/hashicorp/hcl/v2`) and use that version on the right-hand side of the `replace`.

- [ ] **Step 3: Verify build and vet**

Run:
```bash
go build ./...
go vet ./...
```
Expected: both exit 0 with no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum .gitignore main.go
git commit -m "chore: initialize module and dependencies"
```

---

## Task 2: Provider source address parsing

**Files:**
- Create: `internal/registry/addr.go`, `internal/registry/addr_test.go`

**Interfaces:**
- Produces: `registry.ProviderAddr{Host, Namespace, Type string}` with `String() string`; `registry.ParseProviderSource(raw, registryFlag string) (ProviderAddr, error)`; `registry.DefaultRegistry` constant. Consumed by Tasks 4, 5, 7, 10, 11.

- [ ] **Step 1: Write the failing test**

`internal/registry/addr_test.go`:
```go
package registry

import "testing"

func TestParseProviderSource(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		registry   string
		wantAddr   ProviderAddr
		wantString string
	}{
		{"three parts uses embedded host", "registry.opentofu.org/hashicorp/aws", "", ProviderAddr{"registry.opentofu.org", "hashicorp", "aws"}, "registry.opentofu.org/hashicorp/aws"},
		{"two parts uses default host", "hashicorp/aws", "", ProviderAddr{DefaultRegistry, "hashicorp", "aws"}, "registry.opentofu.org/hashicorp/aws"},
		{"registry flag fills two parts", "hashicorp/aws", "registry.terraform.io", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
		{"registry flag overrides embedded host", "registry.opentofu.org/hashicorp/aws", "registry.terraform.io", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
		{"registry flag with scheme is normalized", "hashicorp/aws", "https://registry.terraform.io/", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseProviderSource(tc.raw, tc.registry)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantAddr {
				t.Errorf("got %+v, want %+v", got, tc.wantAddr)
			}
			if got.String() != tc.wantString {
				t.Errorf("String() = %q, want %q", got.String(), tc.wantString)
			}
		})
	}
}

func TestParseProviderSourceErrors(t *testing.T) {
	for _, raw := range []string{"", "aws", "a/b/c/d", "/hashicorp/aws", "hashicorp/"} {
		if _, err := ParseProviderSource(raw, ""); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: ProviderAddr` / `ParseProviderSource`.

- [ ] **Step 3: Write the implementation**

`internal/registry/addr.go`:
```go
package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultRegistry is used when an address omits a host and no --registry flag is set.
const DefaultRegistry = "registry.opentofu.org"

// ProviderAddr is a parsed provider source address: hostname/namespace/type.
type ProviderAddr struct {
	Host      string
	Namespace string
	Type      string
}

// String returns the canonical "host/namespace/type" form used as the provider
// block label in .terraform.lock.hcl.
func (a ProviderAddr) String() string {
	return a.Host + "/" + a.Namespace + "/" + a.Type
}

// BaseURL returns the HTTPS base URL of the registry host.
func (a ProviderAddr) BaseURL() string {
	return "https://" + a.Host
}

var segmentRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ParseProviderSource parses a raw source address and resolves the effective
// registry host. A non-empty registryFlag overrides any host embedded in the
// address. A two-segment address (namespace/type) uses the default host.
func ParseProviderSource(raw string, registryFlag string) (ProviderAddr, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "/")
	var embedded, ns, typ string
	switch len(parts) {
	case 3:
		embedded, ns, typ = strings.TrimSpace(parts[0]), parts[1], parts[2]
	case 2:
		ns, typ = parts[0], parts[1]
	default:
		return ProviderAddr{}, fmt.Errorf("invalid provider source address %q: expected [host/]namespace/type", raw)
	}
	host := embedded
	if registryFlag != "" {
		host = normalizeHost(registryFlag)
	}
	if host == "" {
		host = DefaultRegistry
	}
	if err := validateSegment("namespace", ns); err != nil {
		return ProviderAddr{}, err
	}
	if err := validateSegment("type", typ); err != nil {
		return ProviderAddr{}, err
	}
	return ProviderAddr{Host: host, Namespace: ns, Type: typ}, nil
}

func validateSegment(name, v string) error {
	if !segmentRe.MatchString(v) {
		return fmt.Errorf("invalid provider %s %q: must match [A-Za-z0-9_-]+", name, v)
	}
	return nil
}

// normalizeHost strips a scheme and trailing slash from a registry value.
func normalizeHost(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.TrimSuffix(s, "/")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/addr.go internal/registry/addr_test.go
git commit -m "feat(registry): parse provider source addresses"
```

---

## Task 3: Version selection

**Files:**
- Create: `internal/registry/version.go`, `internal/registry/version_test.go`

**Interfaces:**
- Consumes: nothing beyond `go-version`.
- Produces: `registry.SelectVersion(all []string, requested string) (string, error)`. Consumed by Task 11.

- [ ] **Step 1: Write the failing test**

`internal/registry/version_test.go`:
```go
package registry

import "testing"

func TestSelectVersion(t *testing.T) {
	tests := []struct {
		name      string
		all       []string
		requested string
		want      string
	}{
		{"latest non-prerelease", []string{"4.0.0", "6.0.0", "5.0.0"}, "", "6.0.0"},
		{"prerelease excluded from latest", []string{"6.0.0-rc1", "5.0.0"}, "", "5.0.0"},
		{"explicit version", []string{"4.0.0", "5.0.0"}, "5.0.0", "5.0.0"},
		{"explicit prerelease allowed", []string{"6.0.0-rc1", "5.0.0"}, "6.0.0-rc1", "6.0.0-rc1"},
		{"leading v stripped", []string{"v2.0.0"}, "2.0.0", "2.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectVersion(tc.all, tc.requested)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSelectVersionErrors(t *testing.T) {
	if _, err := SelectVersion(nil, ""); err == nil {
		t.Error("expected error for empty list")
	}
	if _, err := SelectVersion([]string{"5.0.0"}, "9.9.9"); err == nil {
		t.Error("expected error for not-found version")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: SelectVersion`.

- [ ] **Step 3: Write the implementation**

`internal/registry/version.go`:
```go
package registry

import (
	"fmt"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// SelectVersion picks the version to pin. If requested is non-empty, it must be
// present in all. Otherwise the highest non-prerelease version is chosen. A
// leading "v" is stripped defensively.
func SelectVersion(all []string, requested string) (string, error) {
	if len(all) == 0 {
		return "", fmt.Errorf("no versions available")
	}
	var versions []*goversion.Version
	for _, raw := range all {
		if v, err := goversion.NewVersion(strings.TrimPrefix(raw, "v")); err == nil {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no parseable versions in %v", all)
	}
	if requested != "" {
		target, err := goversion.NewVersion(strings.TrimPrefix(requested, "v"))
		if err != nil {
			return "", fmt.Errorf("invalid requested version %q: %w", requested, err)
		}
		for _, v := range versions {
			if v.Equal(target) {
				return v.String(), nil
			}
		}
		return "", fmt.Errorf("version %q not found; available: %v", requested, all)
	}
	goversion.Sort(versions)
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Prerelease() == "" {
			return versions[i].String(), nil
		}
	}
	return "", fmt.Errorf("only prerelease versions available: %v", all)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/version.go internal/registry/version_test.go
git commit -m "feat(registry): select provider version"
```

---

## Task 4: Registry client — ListVersions

**Files:**
- Create: `internal/registry/client.go`, `internal/registry/client_test.go`
- Create: `testdata/registry/versions.json`

**Interfaces:**
- Consumes: `registry.ProviderAddr` (Task 2).
- Produces: `registry.NewClient(hc *http.Client) *Client` and `(*Client).ListVersions(ctx, addr) ([]string, error)`. Consumed by Tasks 7 and 11.

- [ ] **Step 1: Create the fixture**

`testdata/registry/versions.json`:
```json
{
  "versions": [
    {"version": "5.0.0", "protocols": ["5.0"], "platforms": [{"os": "linux", "arch": "amd64"}]},
    {"version": "6.0.0", "protocols": ["5.0"], "platforms": [{"os": "linux", "arch": "amd64"}, {"os": "darwin", "arch": "arm64"}]}
  ],
  "warnings": null
}
```

- [ ] **Step 2: Write the failing test**

`internal/registry/client_test.go`:
```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: NewClient` / `Client.ListVersions`.

- [ ] **Step 4: Write the implementation**

`internal/registry/client.go`:
```go
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client talks the providers.v1 registry protocol over HTTPS.
type Client struct {
	httpClient *http.Client
}

// NewClient returns a registry client. If hc is nil, http.DefaultClient is used.
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{httpClient: hc}
}

type versionsResponse struct {
	Versions []struct {
		Version   string `json:"version"`
		Protocols []string
		Platforms []struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
		}
	} `json:"versions"`
}

// ListVersions returns the raw version strings published for the provider.
func (c *Client) ListVersions(ctx context.Context, addr ProviderAddr) ([]string, error) {
	u := addr.BaseURL() + "/v1/providers/" + addr.Namespace + "/" + addr.Type + "/versions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s: %s", u, resp.Status)
	}
	var body versionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding versions from %s: %w", u, err)
	}
	out := make([]string, 0, len(body.Versions))
	for _, v := range body.Versions {
		out = append(out, v.Version)
	}
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/registry/client.go internal/registry/client_test.go testdata/registry/versions.json
git commit -m "feat(registry): list provider versions"
```

---

## Task 5: Registry client — PackageMeta (with OpenTofu packages extension)

**Files:**
- Extend: `internal/registry/client.go` (append methods/types)
- Extend: `internal/registry/client_test.go` (append tests)
- Create: `testdata/registry/download-packages.json`, `testdata/registry/download-plain.json`

**Interfaces:**
- Consumes: `registry.ProviderAddr` (Task 2), `registry.NewClient` (Task 4).
- Produces: `registry.PackageMeta` struct and `(*Client).PackageMeta(ctx, addr, version, os, arch) (*PackageMeta, error)` (with `Packages` map and resolved `ShasumsURL`). Consumed by Task 7.

- [ ] **Step 1: Create the fixtures**

`testdata/registry/download-packages.json`:
```json
{
  "protocols": ["5.0"],
  "os": "linux",
  "arch": "amd64",
  "filename": "terraform-provider-aws_6.0.0_linux_amd64.zip",
  "download_url": "https://example.com/aws/6.0.0/terraform-provider-aws_6.0.0_linux_amd64.zip",
  "shasums_url": "/v1/providers/hashicorp/aws/6.0.0/shasums",
  "shasums_signature_url": "/v1/providers/hashicorp/aws/6.0.0/shasums.sig",
  "shasum": "aaaa",
  "signing_keys": {"gpg_public_keys": []},
  "packages": {
    "linux_amd64": {"hashes": ["zh:94b25024bfc5c37d725c6cb21b3ae3c2dc8fea9fcce10b0cf272e60bae464dc3", "h1:o2/NmQSjFp/wbXAzs7FUgjNTgjGAmUe/MPHhST8lMtA="], "package_size": 100},
    "darwin_arm64": {"hashes": ["zh:abcdef", "h1:bbbb"], "package_size": 90}
  }
}
```

`testdata/registry/download-plain.json` (no `packages` field, HashiCorp-style):
```json
{
  "protocols": ["5.0"],
  "os": "linux",
  "arch": "amd64",
  "filename": "terraform-provider-aws_6.0.0_linux_amd64.zip",
  "download_url": "https://releases.example.com/aws/6.0.0/terraform-provider-aws_6.0.0_linux_amd64.zip",
  "shasums_url": "/SHA256SUMS",
  "shasums_signature_url": "https://releases.example.com/aws/6.0.0/SHA256SUMS.sig",
  "shasum": "aaaa",
  "signing_keys": {"gpg_public_keys": []}
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/registry/client_test.go`:
```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: PackageMeta` (type and method).

- [ ] **Step 4: Write the implementation**

Append to `internal/registry/client.go`:
```go
import (
	// add "io" and "net/url" to the existing import block
)

// PackageFile is one platform's entry in the OpenTofu packages extension.
type PackageFile struct {
	Hashes      []string `json:"hashes"`
	PackageSize int64    `json:"package_size"`
}

// PackageMeta is the registry download-endpoint response.
type PackageMeta struct {
	Filename      string                 `json:"filename"`
	DownloadURL   string                 `json:"download_url"`
	ShasumsURL    string                 `json:"shasums_url"`
	ShasumsSigURL string                 `json:"shasums_signature_url"`
	Shasum        string                 `json:"shasum"`
	SigningKeys   json.RawMessage        `json:"signing_keys"`
	Packages      map[string]PackageFile `json:"packages"`
}

// PackageMeta fetches package metadata for a version and platform.
func (c *Client) PackageMeta(ctx context.Context, addr ProviderAddr, version, osName, arch string) (*PackageMeta, error) {
	u := addr.BaseURL() + "/v1/providers/" + addr.Namespace + "/" + addr.Type + "/" + version + "/download/" + osName + "/" + arch
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s: %s", u, resp.Status)
	}
	var meta PackageMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decoding package meta from %s: %w", u, err)
	}
	base := resp.Request.URL
	meta.ShasumsURL = resolveURL(base, meta.ShasumsURL)
	meta.DownloadURL = resolveURL(base, meta.DownloadURL)
	meta.ShasumsSigURL = resolveURL(base, meta.ShasumsSigURL)
	return &meta, nil
}

// FetchSHASUMS downloads the signed SHASUMS document at urlStr.
func (c *Client) FetchSHASUMS(ctx context.Context, urlStr string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching shasums %s: %s", urlStr, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func resolveURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(parsed).String()
}
```

Update the `import` block at the top of `client.go` to include `"io"` and `"net/url"` alongside the existing imports.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/registry/client.go internal/registry/client_test.go testdata/registry/download-packages.json testdata/registry/download-plain.json
git commit -m "feat(registry): fetch package metadata with packages extension"
```

---

## Task 6: SHASUMS parsing

**Files:**
- Create: `internal/registry/shasums.go`, `internal/registry/shasums_test.go`

**Interfaces:**
- Produces: `registry.SHASUMLine{Hex, Filename string}`, `registry.ParseSHASUMSLines(body []byte) []SHASUMLine`, `registry.ParseSHASUMS(body []byte, platforms []string) []string` (returns `zh:` hashes filtered to platforms). Consumed by Task 7.

- [ ] **Step 1: Write the failing test**

`internal/registry/shasums_test.go`:
```go
package registry

import (
	"reflect"
	"testing"
)

func TestParseSHASUMSLines(t *testing.T) {
	body := []byte("aaaa  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		"bbbb  terraform-provider-aws_6.0.0_darwin_arm64.zip\n" +
		"\n" +
		"cccc  terraform-provider-aws_6.0.0_windows_amd64.zip\n")
	got := ParseSHASUMSLines(body)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(got), got)
	}
	if got[0].Hex != "aaaa" || got[0].Filename != "terraform-provider-aws_6.0.0_linux_amd64.zip" {
		t.Errorf("first line wrong: %+v", got[0])
	}
}

func TestParseSHASUMS(t *testing.T) {
	body := []byte("aaaa  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		"bbbb  terraform-provider-aws_6.0.0_darwin_arm64.zip\n")
	got := ParseSHASUMS(body, []string{"linux_amd64"})
	want := []string{"zh:aaaa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got2 := ParseSHASUMS(body, []string{"linux_amd64", "darwin_arm64"})
	want2 := []string{"zh:aaaa", "zh:bbbb"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("got %v, want %v", got2, want2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: ParseSHASUMSLines` / `ParseSHASUMS`.

- [ ] **Step 3: Write the implementation**

`internal/registry/shasums.go`:
```go
package registry

import "strings"

// SHASUMLine is one parsed line of a sha256sum document.
type SHASUMLine struct {
	Hex      string
	Filename string
}

// ParseSHASUMSLines parses every non-empty line of a sha256sum document.
func ParseSHASUMSLines(body []byte) []SHASUMLine {
	var out []SHASUMLine
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, SHASUMLine{Hex: fields[0], Filename: fields[len(fields)-1]})
	}
	return out
}

// ParseSHASUMS returns "zh:"-prefixed hashes for the given platforms only.
// A platform token like "linux_amd64" matches filenames containing "_linux_amd64".
func ParseSHASUMS(body []byte, platforms []string) []string {
	want := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		want["_"+p] = true
	}
	var out []string
	for _, ln := range ParseSHASUMSLines(body) {
		if matchesPlatform(ln.Filename, want) {
			out = append(out, "zh:"+ln.Hex)
		}
	}
	return out
}

func matchesPlatform(filename string, want map[string]bool) bool {
	for token := range want {
		if strings.Contains(filename, token) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/shasums.go internal/registry/shasums_test.go
git commit -m "feat(registry): parse SHASUMS documents"
```

---

## Task 7: Hash resolver with in-memory cache

**Files:**
- Create: `internal/registry/resolver.go`, `internal/registry/resolver_test.go`
- Create: `testdata/registry/shasums.txt`

**Interfaces:**
- Consumes: `registry.ProviderAddr` (Task 2), `registry.Client` with `PackageMeta`/`FetchSHASUMS` (Tasks 4–5), `registry.ParseSHASUMSLines` (Task 6).
- Produces: `registry.NewResolver(c *Client) *Resolver` and `(*Resolver).Hashes(ctx, addr, version, platforms) ([]string, error)`. Consumed by Task 11.

- [ ] **Step 1: Create the fixture**

`testdata/registry/shasums.txt`:
```
aaaa  terraform-provider-aws_6.0.0_linux_amd64.zip
bbbb  terraform-provider-aws_6.0.0_darwin_arm64.zip
cccc  terraform-provider-aws_6.0.0_windows_amd64.zip
```

- [ ] **Step 2: Write the failing test**

`internal/registry/resolver_test.go`:
```go
package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/registry/`
Expected: FAIL — `undefined: NewResolver` / `Resolver.Hashes`.

- [ ] **Step 4: Write the implementation**

`internal/registry/resolver.go`:
```go
package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Resolver resolves lock-file hashes for a provider version, caching results so
// the registry is hit at most once per (host, namespace, type, version).
type Resolver struct {
	client *Client
	mu     sync.Mutex
	cache  map[string]cachedHashes
}

type cachedHashes struct {
	byPlatform  map[string][]string // OpenTofu packages path: "os_arch" -> hashes
	shasumLines []SHASUMLine        // SHASUMS path: all lines, filtered at read
}

// NewResolver returns a Resolver backed by the given client.
func NewResolver(c *Client) *Resolver {
	return &Resolver{client: c, cache: map[string]cachedHashes{}}
}

// Hashes resolves the lock-file hashes for addr@version on the given platforms.
func (r *Resolver) Hashes(ctx context.Context, addr ProviderAddr, version string, platforms []string) ([]string, error) {
	if len(platforms) == 0 {
		return nil, fmt.Errorf("no platforms requested")
	}
	key := addr.Host + "/" + addr.Namespace + "/" + addr.Type + "@" + version
	r.mu.Lock()
	c, ok := r.cache[key]
	r.mu.Unlock()
	if !ok {
		var err error
		c, err = r.fetch(ctx, addr, version, platforms[0])
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.cache[key] = c
		r.mu.Unlock()
	}
	return dedupSort(c.hashesFor(platforms)), nil
}

func (r *Resolver) fetch(ctx context.Context, addr ProviderAddr, version, firstPlatform string) (cachedHashes, error) {
	osName, arch := splitPlatform(firstPlatform)
	if osName == "" || arch == "" {
		return cachedHashes{}, fmt.Errorf("invalid platform %q", firstPlatform)
	}
	meta, err := r.client.PackageMeta(ctx, addr, version, osName, arch)
	if err != nil {
		return cachedHashes{}, err
	}
	if len(meta.Packages) > 0 {
		byPlatform := make(map[string][]string, len(meta.Packages))
		for plat, pf := range meta.Packages {
			byPlatform[plat] = pf.Hashes
		}
		return cachedHashes{byPlatform: byPlatform}, nil
	}
	body, err := r.client.FetchSHASUMS(ctx, meta.ShasumsURL)
	if err != nil {
		return cachedHashes{}, err
	}
	return cachedHashes{shasumLines: ParseSHASUMSLines(body)}, nil
}

func (c cachedHashes) hashesFor(platforms []string) []string {
	var out []string
	if c.byPlatform != nil {
		for _, p := range platforms {
			out = append(out, c.byPlatform[p]...)
		}
	} else {
		want := make(map[string]bool, len(platforms))
		for _, p := range platforms {
			want["_"+p] = true
		}
		for _, ln := range c.shasumLines {
			if matchesPlatform(ln.Filename, want) {
				out = append(out, "zh:"+ln.Hex)
			}
		}
	}
	return out
}

func splitPlatform(p string) (osName, arch string) {
	parts := strings.SplitN(p, "_", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func dedupSort(hashes []string) []string {
	seen := make(map[string]bool, len(hashes))
	out := hashes[:0]
	for _, h := range hashes {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if scheme(out[i]) != scheme(out[j]) {
			return scheme(out[i]) < scheme(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func scheme(h string) string {
	return strings.SplitN(h, ":", 2)[0]
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/registry/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/registry/resolver.go internal/registry/resolver_test.go testdata/registry/shasums.txt
git commit -m "feat(registry): resolve hashes with in-memory cache"
```

---

## Task 8: Lock file render and merge

**Files:**
- Create: `internal/lockfile/lockfile.go`, `internal/lockfile/lockfile_test.go`

**Interfaces:**
- Consumes: nothing (self-contained HCL helpers).
- Produces: `lockfile.ProviderAttrs{Version, Constraints string; Hashes []string}`, `lockfile.RenderProviderBlock(addr string, attrs ProviderAttrs) []byte`, `lockfile.MergeProviderBlock(data []byte, addr string, attrs ProviderAttrs) ([]byte, error)`. Consumed by Tasks 9 and 11.

- [ ] **Step 1: Write the failing test**

`internal/lockfile/lockfile_test.go`:
```go
package lockfile

import (
	"strings"
	"testing"
)

func TestRenderProviderBlock(t *testing.T) {
	out := RenderProviderBlock("registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version:     "6.0.0",
		Constraints: "~> 6.0",
		Hashes:      []string{"h1:aaa=", "zh:bbbb"},
	})
	s := string(out)
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/aws"`) {
		t.Errorf("missing provider header: %s", s)
	}
	if !strings.Contains(s, "version = ") || !strings.Contains(s, `"6.0.0"`) {
		t.Errorf("missing version: %s", s)
	}
	if !strings.Contains(s, "h1:aaa=") || !strings.Contains(s, "zh:bbbb") {
		t.Errorf("missing hashes: %s", s)
	}
}

func TestMergeProviderBlockPreservesUnspecified(t *testing.T) {
	existing := []byte(`provider "registry.opentofu.org/hashicorp/aws" {
  version     = "5.0.0"
  constraints = "~> 5.0"
  hashes = [
    "h1:old=",
  ]
}
`)
	out, err := MergeProviderBlock(existing, "registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version: "6.0.0",
		Hashes:  []string{"h1:new="},
		// Constraints intentionally empty: must be preserved.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"6.0.0"`) || strings.Contains(s, `"5.0.0"`) {
		t.Errorf("version not updated: %s", s)
	}
	if !strings.Contains(s, `constraints = "~> 5.0"`) {
		t.Errorf("existing constraints not preserved: %s", s)
	}
	if !strings.Contains(s, "h1:new=") || strings.Contains(s, "h1:old=") {
		t.Errorf("hashes not replaced: %s", s)
	}
}

func TestMergeProviderBlockAppendsNewAndPreservesOthers(t *testing.T) {
	existing := []byte(`# top comment
provider "registry.opentofu.org/hashicorp/random" {
  version = "3.0.0"
  hashes  = ["zh:rrr"]
}
`)
	out, err := MergeProviderBlock(existing, "registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version: "6.0.0",
		Hashes:  []string{"h1:new="},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "# top comment") {
		t.Errorf("comment not preserved: %s", s)
	}
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/random"`) {
		t.Errorf("other provider not preserved: %s", s)
	}
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/aws"`) {
		t.Errorf("new provider not appended: %s", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/`
Expected: FAIL — `undefined: RenderProviderBlock` / `MergeProviderBlock`.

- [ ] **Step 3: Write the implementation**

`internal/lockfile/lockfile.go`:
```go
package lockfile

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// ProviderAttrs holds the attributes the tool may set on a provider block.
// A zero-valued Constraints or empty Hashes slice means "do not touch".
type ProviderAttrs struct {
	Version     string
	Constraints string
	Hashes      []string
}

func applyAttrs(body *hclwrite.Body, attrs ProviderAttrs) {
	if attrs.Version != "" {
		body.SetAttributeValue("version", cty.StringVal(attrs.Version))
	}
	if attrs.Constraints != "" {
		body.SetAttributeValue("constraints", cty.StringVal(attrs.Constraints))
	}
	if len(attrs.Hashes) > 0 {
		vals := make([]cty.Value, 0, len(attrs.Hashes))
		for _, h := range attrs.Hashes {
			vals = append(vals, cty.StringVal(h))
		}
		body.SetAttributeRaw("hashes", hclwrite.TokensForValue(cty.ListVal(vals)))
	}
}

// RenderProviderBlock renders a single standalone provider {} block.
func RenderProviderBlock(addr string, attrs ProviderAttrs) []byte {
	f := hclwrite.NewEmptyFile()
	b := f.Body().AppendNewBlock("provider", []string{addr})
	applyAttrs(b.Body(), attrs)
	return f.Bytes()
}

// MergeProviderBlock merges attrs into the provider block for addr within data.
// Attributes not present in attrs are preserved. If no matching block exists, a
// new one is appended. Empty data starts a fresh file.
func MergeProviderBlock(data []byte, addr string, attrs ProviderAttrs) ([]byte, error) {
	f, diags := hclwrite.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	block := findProviderBlock(f.Body(), addr)
	if block == nil {
		block = f.Body().AppendNewBlock("provider", []string{addr})
	}
	applyAttrs(block.Body(), attrs)
	out := f.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

func findProviderBlock(body *hclwrite.Body, addr string) *hclwrite.Block {
	for _, b := range body.Blocks() {
		if b.Type() == "provider" && len(b.Labels()) == 1 && b.Labels()[0] == addr {
			return b
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/lockfile.go internal/lockfile/lockfile_test.go
git commit -m "feat(lockfile): render and merge provider blocks"
```

---

## Task 9: Verbatim block decoding

**Files:**
- Create: `internal/lockfile/verbatim.go`, `internal/lockfile/verbatim_test.go`

**Interfaces:**
- Consumes: `lockfile.ProviderAttrs` (Task 8).
- Produces: `lockfile.DecodeVerbatimBlock(data []byte) (addr string, attrs ProviderAttrs, err error)`. Consumed by Task 11.

- [ ] **Step 1: Write the failing test**

`internal/lockfile/verbatim_test.go`:
```go
package lockfile

import "testing"

const cloudflareBlock = `provider "registry.opentofu.org/cloudflare/cloudflare" {
  version     = "5.22.0"
  constraints = "5.22.0"
  hashes = [
    "h1:EOm3pWL+XqsFNVhupvcedWz7XrYurk52G6ElTSJ+Fxk=",
    "zh:45f3b7c50254b1da1dc21e77e03cd1e931cab40fb75c7cba822a53ed54cd232e",
  ]
}
`

func TestDecodeVerbatimBlock(t *testing.T) {
	addr, attrs, err := DecodeVerbatimBlock([]byte(cloudflareBlock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "registry.opentofu.org/cloudflare/cloudflare" {
		t.Errorf("addr = %q", addr)
	}
	if attrs.Version != "5.22.0" {
		t.Errorf("version = %q", attrs.Version)
	}
	if attrs.Constraints != "5.22.0" {
		t.Errorf("constraints = %q", attrs.Constraints)
	}
	if len(attrs.Hashes) != 2 {
		t.Errorf("hashes = %v", attrs.Hashes)
	}
}

func TestDecodeVerbatimBlockErrors(t *testing.T) {
	cases := map[string]string{
		"no provider block":    `terraform { required_version = ">= 1.0" }`,
		"two provider blocks":  `provider "a/b/c" { version = "1.0.0" hashes = ["zh:1"] }\nprovider "x/y/z" { version = "1.0.0" hashes = ["zh:2"] }`,
		"missing version":      `provider "a/b/c" { hashes = ["zh:1"] }`,
		"missing hashes":       `provider "a/b/c" { version = "1.0.0" }`,
		"malformed hcl":        `provider "a/b/c" { version = `,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeVerbatimBlock([]byte(src)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/`
Expected: FAIL — `undefined: DecodeVerbatimBlock`.

- [ ] **Step 3: Write the implementation**

`internal/lockfile/verbatim.go`:
```go
package lockfile

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type verbatimFile struct {
	Providers []verbatimProvider `hcl:"provider,block"`
}

type verbatimProvider struct {
	Address     string   `hcl:",label"`
	Version     string   `hcl:"version"`
	Constraints string   `hcl:"constraints,optional"`
	Hashes      []string `hcl:"hashes,optional"`
}

// DecodeVerbatimBlock decodes a file expected to hold exactly one provider
// block and returns its label and attributes.
func DecodeVerbatimBlock(data []byte) (string, ProviderAttrs, error) {
	file, diags := hclsyntax.ParseConfig(data, "block.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return "", ProviderAttrs{}, diags
	}
	var vf verbatimFile
	if diags := gohcl.DecodeBody(file.Body, nil, &vf); diags.HasErrors() {
		return "", ProviderAttrs{}, diags
	}
	if len(vf.Providers) == 0 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim block file contains no provider block")
	}
	if len(vf.Providers) > 1 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim block file must contain exactly one provider block, found %d", len(vf.Providers))
	}
	p := vf.Providers[0]
	if p.Version == "" {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim provider block is missing version")
	}
	if len(p.Hashes) == 0 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim provider block is missing hashes")
	}
	return p.Address, ProviderAttrs{Version: p.Version, Constraints: p.Constraints, Hashes: p.Hashes}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/verbatim.go internal/lockfile/verbatim_test.go
git commit -m "feat(lockfile): decode verbatim provider blocks"
```

---

## Task 10: CLI configuration and argument parsing

**Files:**
- Create: `internal/cli/config.go`, `internal/cli/config_test.go`

**Interfaces:**
- Consumes: nothing external.
- Produces: `cli.Mode` constants, `cli.Config` struct, `cli.ParseArgs(args []string) (Config, error)`. Consumed by Task 11.

- [ ] **Step 1: Write the failing test**

`internal/cli/config_test.go`:
```go
package cli

import (
	"runtime"
	"testing"
)

func TestParseArgsLookup(t *testing.T) {
	cfg, err := ParseArgs([]string{
		"registry.opentofu.org/hashicorp/aws",
		"--version", "6.0.0",
		"--platform", "linux_amd64",
		"--platform", "darwin_arm64",
		"--constraints", "~> 6.0",
		"--registry", "registry.terraform.io",
		"a.lock.hcl", "b.lock.hcl",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeLookup {
		t.Errorf("Mode = %v, want ModeLookup", cfg.Mode)
	}
	if cfg.ProviderRaw != "registry.opentofu.org/hashicorp/aws" {
		t.Errorf("ProviderRaw = %q", cfg.ProviderRaw)
	}
	if cfg.Version != "6.0.0" || cfg.Constraints != "~> 6.0" || cfg.Registry != "registry.terraform.io" {
		t.Errorf("flags wrong: %+v", cfg)
	}
	if len(cfg.Platforms) != 2 {
		t.Errorf("Platforms = %v", cfg.Platforms)
	}
	if len(cfg.LockFiles) != 2 {
		t.Errorf("LockFiles = %v", cfg.LockFiles)
	}
}

func TestParseArgsDefaultPlatform(t *testing.T) {
	cfg, err := ParseArgs([]string{"hashicorp/aws", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := runtime.GOOS + "_" + runtime.GOARCH
	if len(cfg.Platforms) != 1 || cfg.Platforms[0] != want {
		t.Errorf("Platforms = %v, want [%s]", cfg.Platforms, want)
	}
}

func TestParseArgsVerbatim(t *testing.T) {
	cfg, err := ParseArgs([]string{"--block-file", "aws.lock.hcl", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeVerbatim || cfg.BlockFile != "aws.lock.hcl" {
		t.Errorf("verbatim config wrong: %+v", cfg)
	}
	if len(cfg.LockFiles) != 1 || cfg.LockFiles[0] != "a.lock.hcl" {
		t.Errorf("LockFiles = %v", cfg.LockFiles)
	}
}

func TestParseArgsErrors(t *testing.T) {
	cases := map[string][]string{
		"missing provider source": {"--version", "1.0.0"},
		"missing lock file":       {"hashicorp/aws"},
		"invalid platform":        {"hashicorp/aws", "--platform", "bogus", "a.lock.hcl"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseArgs(args); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParseArgsPrintBlockNoFiles(t *testing.T) {
	if _, err := ParseArgs([]string{"hashicorp/aws", "--print-block"}); err != nil {
		t.Fatalf("print-block should allow no files: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/`
Expected: FAIL — `undefined: Config` / `ParseArgs`.

- [ ] **Step 3: Write the implementation**

`internal/cli/config.go`:
```go
package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/pflag"
)

// Mode selects the input shape.
type Mode int

const (
	ModeLookup Mode = iota
	ModeVerbatim
)

// Config is the parsed CLI configuration.
type Config struct {
	Mode        Mode
	BlockFile   string   // verbatim mode
	ProviderRaw string   // lookup mode raw address
	Version     string
	Platforms   []string
	Constraints string
	Registry    string
	PrintBlock  bool
	LockFiles   []string
}

// ParseArgs parses argv (without the program name) into a Config.
func ParseArgs(args []string) (Config, error) {
	fs := pflag.NewFlagSet("terragrunt-providers-pin", pflag.ContinueOnError)
	var version, constraints, registry, blockFile string
	var platforms []string
	var printBlock bool

	fs.StringVar(&version, "version", "", "exact version to pin")
	fs.StringArrayVar(&platforms, "platform", nil, "target platform os_arch (repeatable)")
	fs.StringVar(&constraints, "constraints", "", "set or replace the constraints attribute")
	fs.StringVar(&registry, "registry", "", "registry host to query")
	fs.StringVar(&blockFile, "block-file", "", "file containing one provider block (verbatim mode)")
	fs.BoolVar(&printBlock, "print-block", false, "print the resolved provider block to stdout")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	rest := fs.Args()
	cfg := Config{
		Version: version, Platforms: platforms, Constraints: constraints,
		Registry: registry, BlockFile: blockFile, PrintBlock: printBlock,
	}

	if blockFile != "" {
		cfg.Mode = ModeVerbatim
		cfg.LockFiles = rest
	} else {
		cfg.Mode = ModeLookup
		if len(rest) == 0 {
			return Config{}, fmt.Errorf("missing provider source address (or use --block-file)")
		}
		cfg.ProviderRaw = rest[0]
		cfg.LockFiles = rest[1:]
		if len(cfg.Platforms) == 0 {
			cfg.Platforms = []string{runtime.GOOS + "_" + runtime.GOARCH}
		}
		for _, p := range cfg.Platforms {
			if !validPlatform(p) {
				return Config{}, fmt.Errorf("invalid platform %q: expected os_arch", p)
			}
		}
	}

	if !cfg.PrintBlock && len(cfg.LockFiles) == 0 {
		return Config{}, fmt.Errorf("at least one lock file is required (or use --print-block)")
	}
	return cfg, nil
}

func validPlatform(p string) bool {
	parts := strings.SplitN(p, "_", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go
git commit -m "feat(cli): parse and validate arguments"
```

---

## Task 11: Orchestration and main wiring (end-to-end)

**Files:**
- Create: `internal/cli/run.go`, `internal/cli/run_test.go`
- Replace: `main.go`

**Interfaces:**
- Consumes: `cli.Config`/`ParseArgs` (Task 10), `registry` package (Tasks 2–7), `lockfile` package (Tasks 8–9).
- Produces: `cli.Run(ctx, cfg, deps) error` and the runnable `main.go`.

- [ ] **Step 1: Write the failing end-to-end test**

`internal/cli/run_test.go`:
```go
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
	os.WriteFile(lockPath, []byte(`provider "registry.opentofu.org/hashicorp/aws" {
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
		[]byte(`{"versions":[{"version":"6.0.0"}]}`),
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
		[]byte(`{"versions":[{"version":"6.0.0"}]}`),
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
	if !strings.Contains(out.String(), `provider "`) || !strings.Contains(out.String(), "zh:aaa") {
		t.Errorf("printed block wrong:\n%s", out.String())
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/`
Expected: FAIL — `undefined: Run` / `Deps`.

- [ ] **Step 3: Write the implementation**

`internal/cli/run.go`:
```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nijave/terragrunt-providers-pin/internal/lockfile"
	"github.com/nijave/terragrunt-providers-pin/internal/registry"
)

// Lister lists provider versions.
type Lister interface {
	ListVersions(ctx context.Context, addr registry.ProviderAddr) ([]string, error)
}

// HashResolver resolves lock-file hashes.
type HashResolver interface {
	Hashes(ctx context.Context, addr registry.ProviderAddr, version string, platforms []string) ([]string, error)
}

// Deps holds the collaborators Run needs. Interfaces make Run testable without
// real network access.
type Deps struct {
	Lister   Lister
	Resolver HashResolver
	Stdout   io.Writer
}

// Run executes the configured action.
func Run(ctx context.Context, cfg Config, deps Deps) error {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}

	var addr registry.ProviderAddr
	var attrs lockfile.ProviderAttrs

	switch cfg.Mode {
	case ModeVerbatim:
		data, err := os.ReadFile(cfg.BlockFile)
		if err != nil {
			return fmt.Errorf("reading --block-file %s: %w", cfg.BlockFile, err)
		}
		label, blockAttrs, err := lockfile.DecodeVerbatimBlock(data)
		if err != nil {
			return err
		}
		a, err := registry.ParseProviderSource(label, cfg.Registry)
		if err != nil {
			return err
		}
		addr = a
		attrs = blockAttrs
		if cfg.Constraints != "" {
			attrs.Constraints = cfg.Constraints
		}

	case ModeLookup:
		a, err := registry.ParseProviderSource(cfg.ProviderRaw, cfg.Registry)
		if err != nil {
			return err
		}
		addr = a
		all, err := deps.Lister.ListVersions(ctx, addr)
		if err != nil {
			return err
		}
		version, err := registry.SelectVersion(all, cfg.Version)
		if err != nil {
			return err
		}
		hashes, err := deps.Resolver.Hashes(ctx, addr, version, cfg.Platforms)
		if err != nil {
			return err
		}
		attrs = lockfile.ProviderAttrs{Version: version, Hashes: hashes, Constraints: cfg.Constraints}
	}

	if cfg.PrintBlock {
		fmt.Fprintln(deps.Stdout, string(lockfile.RenderProviderBlock(addr.String(), attrs)))
		return nil
	}

	for _, path := range cfg.LockFiles {
		existing, _ := os.ReadFile(path)
		updated, err := lockfile.MergeProviderBlock(existing, addr.String(), attrs)
		if err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
		if err := atomicWrite(path, updated); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".terraform.lock.hcl.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Replace `main.go`:
```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/nijave/terragrunt-providers-pin/internal/cli"
	"github.com/nijave/terragrunt-providers-pin/internal/registry"
)

func main() {
	cfg, err := cli.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	client := registry.NewClient(nil)
	deps := cli.Deps{
		Lister:   client,
		Resolver: registry.NewResolver(client),
		Stdout:   os.Stdout,
	}
	if err := cli.Run(context.Background(), cfg, deps); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS for all packages.

- [ ] **Step 5: Build the binary**

Run: `go build -o /tmp/tpp .`
Expected: exit 0; binary exists.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/run.go internal/cli/run_test.go main.go
git commit -m "feat(cli): orchestrate modes and wire main"
```

---

## Task 12: README and final verification

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

`README.md`:
```markdown
# terragrunt-providers-pin

Pin a provider (source address, version, and checksum hashes) into one or more
`.terraform.lock.hcl` files, like `tofu providers lock` but driven by explicit
arguments instead of a configuration tree.

## Install

    go install github.com/nijave/terragrunt-providers-pin@latest

## Usage

    terragrunt-providers-pin [--block-file FILE | PROVIDER_SOURCE] [flags] LOCKFILE...

Lookup mode resolves hashes from the provider registry:

    terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --platform darwin_arm64 \
        .terraform.lock.hcl envs/dev/.terraform.lock.hcl

Generate a verbatim block and save it:

    terragrunt-providers-pin registry.opentofu.org/hashicorp/aws \
        --version 6.0.0 --platform linux_amd64 --print-block > aws.lock.hcl

Verbatim mode applies a hand-written block:

    terragrunt-providers-pin --block-file aws.lock.hcl .terraform.lock.hcl

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--version` | latest non-prerelease | exact version to pin |
| `--platform` | runtime `GOOS_GOARCH` | target platform (repeatable) |
| `--constraints` | (unset) | set/replace `constraints`; preserved when unset |
| `--registry` | `registry.opentofu.org` | registry host to query |
| `--block-file` | | verbatim mode: file with one `provider {}` block |
| `--print-block` | false | print the resolved block and exit |

## Behavior

- Attributes the tool does not set are preserved. A lookup run leaves an
  existing `constraints` untouched unless you pass `--constraints`.
- The OpenTofu registry supplies `h1:` and `zh:` hashes directly (one call). For
  registries without that extension (the HashiCorp registry), the tool emits
  `zh:` hashes from the signed SHASUMS document only.
- The registry is queried once per provider+version, even across many lock files.

## License

MPL-2.0.
```

- [ ] **Step 2: Run the full suite, vet, and build**

Run:
```bash
go test ./...
go vet ./...
go build ./...
```
Expected: all pass with no output.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add README"
```

---

## Self-Review Notes

Spec coverage check (spec section -> task):
- §2 CLI surface -> Task 10 (parse), Task 11 (run), Task 12 (docs).
- §3 effective registry host -> Task 2 (ParseProviderSource precedence), Task 11 (passes cfg.Registry).
- §4 merge/write semantics -> Task 8 (MergeProviderBlock preserves unspecified), Task 11 (override sets).
- §5 registry protocol + hash acquisition (both paths, response-driven) -> Tasks 4, 5, 6, 7.
- §6 version/platform selection -> Task 3 (SelectVersion), Task 10 (platform validation/default).
- §7 caching -> Task 7 (resolver cache + hit-count test), Task 11 (cross-file cache test).
- §8 verbatim decoding -> Task 9.
- §9 project structure -> matches the File Structure above.
- §10 dependencies/licenses -> Task 1 (replace to OpenTofu fork, FOSS only).
- §11 error handling -> spread across Tasks 2, 3, 4, 9, 10, 11.
- §12 testing -> each task is TDD; Task 11 is the end-to-end gate.
- §13 out of scope -> no tasks build the excluded features.

Type consistency check: `ProviderAddr`, `ParseProviderSource`, `SelectVersion`, `NewClient`/`ListVersions`/`PackageMeta`/`FetchSHASUMS`, `ParseSHASUMSLines`/`ParseSHASUMS`, `NewResolver`/`Hashes`, `ProviderAttrs`/`RenderProviderBlock`/`MergeProviderBlock`/`DecodeVerbatimBlock`, `Config`/`ParseArgs`/`Run`/`Deps` — names match across producer and consumer tasks.
