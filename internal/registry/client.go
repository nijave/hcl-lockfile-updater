package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultTimeout bounds each registry request when the caller does not
// supply an http.Client.
const defaultTimeout = 60 * time.Second

// Client talks the providers.v1 registry protocol over HTTPS.
type Client struct {
	httpClient *http.Client
}

// NewClient returns a registry client. If hc is nil, a client with a
// 60-second per-request timeout is used.
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{httpClient: hc}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if !isHTTPS(req.URL) {
		return nil, errors.New("refusing non-HTTPS registry URL")
	}
	hc := *c.httpClient
	originalCheckRedirect := hc.CheckRedirect
	hc.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !isHTTPS(next.URL) {
			return errors.New("refusing redirect to non-HTTPS registry URL")
		}
		if originalCheckRedirect != nil {
			return originalCheckRedirect(next, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return hc.Do(req)
}

func isHTTPS(u *url.URL) bool {
	return u != nil && strings.EqualFold(u.Scheme, "https") && u.Host != ""
}

func displayURL(u *url.URL) string {
	if u == nil {
		return "<invalid URL>"
	}
	clean := *u
	clean.User = nil
	clean.RawQuery = ""
	clean.ForceQuery = false
	clean.Fragment = ""
	return clean.String()
}

func requestError(action string, u *url.URL, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s %s: %s: %w", action, displayURL(u), urlErr.Op, urlErr.Err)
	}
	return fmt.Errorf("%s %s: %w", action, displayURL(u), err)
}

// Platform represents one os/arch pair published for a version.
type Platform struct {
	OS   string
	Arch string
}

// String returns "os_arch".
func (p Platform) String() string {
	return p.OS + "_" + p.Arch
}

// Version holds a version string and its published platforms.
type Version struct {
	Version   string
	Platforms []Platform
}

// Versions returns the version strings only.
func Versions(vs []Version) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
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

// ListVersions returns the published versions with their platforms.
func (c *Client) ListVersions(ctx context.Context, addr ProviderAddr) ([]Version, error) {
	u := addr.BaseURL() + "/v1/providers/" + addr.Namespace + "/" + addr.Type + "/versions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, requestError("querying registry", req.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry %s: %s", u, resp.Status)
	}
	var body versionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding versions from %s: %w", u, err)
	}
	out := make([]Version, 0, len(body.Versions))
	for _, v := range body.Versions {
		plats := make([]Platform, 0, len(v.Platforms))
		for _, p := range v.Platforms {
			plats = append(plats, Platform{OS: p.OS, Arch: p.Arch})
		}
		out = append(out, Version{Version: v.Version, Platforms: plats})
	}
	return out, nil
}

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
	resp, err := c.do(req)
	if err != nil {
		return nil, requestError("querying package metadata", req.URL, err)
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

// maxSHASUMSSize caps how large a SHASUMS response may be. Real documents are
// a few KB; anything larger is a broken or hostile registry.
const maxSHASUMSSize = 1 << 20

// FetchSHASUMS downloads the signed SHASUMS document at urlStr.
//
// The URL comes from the registry's download response and is fetched as given:
// the registry is trusted here, the same trust boundary `terraform providers
// lock` applies. Same-origin pinning would reject legitimate hosts (the
// download endpoint redirects to a release CDN before this URL is resolved).
func (c *Client) FetchSHASUMS(ctx context.Context, urlStr string) ([]byte, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, errors.New("registry returned an invalid SHASUMS URL")
	}
	if !isHTTPS(u) {
		return nil, fmt.Errorf("refusing non-HTTPS SHASUMS URL %s", displayURL(u))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating SHASUMS request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, requestError("fetching SHASUMS", req.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching SHASUMS %s: %s", displayURL(resp.Request.URL), resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSHASUMSSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading SHASUMS %s: %w", displayURL(resp.Request.URL), err)
	}
	if len(body) > maxSHASUMSSize {
		return nil, fmt.Errorf("SHASUMS document %s exceeds %d bytes", displayURL(resp.Request.URL), maxSHASUMSSize)
	}
	return body, nil
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
