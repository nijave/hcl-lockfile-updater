package registry

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	// The lock is held across the fetch so concurrent callers block on it and
	// then hit the cache instead of racing to duplicate the registry request.
	// That serializes lookups for different keys too, which is fine at the
	// scale this CLI runs at.
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cache[key]
	if !ok {
		var err error
		c, err = r.fetch(ctx, addr, version, platforms[0])
		if err != nil {
			return nil, err
		}
		r.cache[key] = c
	}
	hashes, err := c.hashesFor(platforms)
	if err != nil {
		return nil, err
	}
	hashes = dedupSort(hashes)
	if err := validateResolvedHashes(hashes); err != nil {
		return nil, err
	}
	return hashes, nil
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
	lines, err := ParseSHASUMSLines(body)
	if err != nil {
		return cachedHashes{}, fmt.Errorf("parsing SHASUMS: %w", err)
	}
	return cachedHashes{shasumLines: lines}, nil
}

func (c cachedHashes) hashesFor(platforms []string) ([]string, error) {
	var out []string
	if c.byPlatform != nil {
		for _, p := range platforms {
			hashes, ok := c.byPlatform[p]
			if !ok {
				return nil, fmt.Errorf("platform %q not present in registry packages response", p)
			}
			if len(hashes) == 0 {
				return nil, fmt.Errorf("platform %q has no hashes in registry packages response", p)
			}
			out = append(out, hashes...)
		}
	} else {
		for _, p := range platforms {
			want := map[string]bool{"_" + p: true}
			found := false
			for _, ln := range c.shasumLines {
				if matchesPlatform(ln.Filename, want) {
					out = append(out, "zh:"+ln.Hex)
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("platform %q not present in SHASUMS document", p)
			}
		}
	}
	return out, nil
}

func validateResolvedHashes(hashes []string) error {
	if len(hashes) == 0 {
		return fmt.Errorf("registry returned no hashes")
	}
	for _, hash := range hashes {
		switch {
		case strings.HasPrefix(hash, "zh:"):
			if !validSHA256Hex(strings.TrimPrefix(hash, "zh:")) {
				return fmt.Errorf("registry returned invalid zh hash %q", hash)
			}
		case strings.HasPrefix(hash, "h1:"):
			digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hash, "h1:"))
			if err != nil || len(digest) != sha256.Size {
				return fmt.Errorf("registry returned invalid h1 hash %q", hash)
			}
		default:
			return fmt.Errorf("registry returned unsupported hash %q", hash)
		}
	}
	return nil
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
