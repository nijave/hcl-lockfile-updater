package registry

import (
	"fmt"
	"sort"
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
	sort.Sort(goversion.Collection(versions))
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Prerelease() == "" {
			return versions[i].String(), nil
		}
	}
	return "", fmt.Errorf("only prerelease versions available: %v", all)
}
