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
	out := make([]string, 0, len(hashes))
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
