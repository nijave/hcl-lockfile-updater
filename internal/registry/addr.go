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
	// Reject addresses with leading slash (e.g., "/hashicorp/aws")
	if strings.HasPrefix(raw, "/") {
		return ProviderAddr{}, fmt.Errorf("invalid provider source address %q: must not start with '/'", raw)
	}
	// Reject addresses with trailing slash (e.g., "hashicorp/")
	if strings.HasSuffix(raw, "/") {
		return ProviderAddr{}, fmt.Errorf("invalid provider source address %q: must not end with '/'", raw)
	}
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
