package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
