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
