package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nijave/hcl-lockfile-updater/internal/lockfile"
	"github.com/nijave/hcl-lockfile-updater/internal/registry"
)

// Lister lists provider versions with their platforms.
type Lister interface {
	ListVersions(ctx context.Context, addr registry.ProviderAddr) ([]registry.Version, error)
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
		allVersions, err := deps.Lister.ListVersions(ctx, addr)
		if err != nil {
			return err
		}
		allStrings := registry.Versions(allVersions)
		version, err := registry.SelectVersion(allStrings, cfg.Version)
		if err != nil {
			return err
		}
		// Validate that every requested platform is available for the chosen version.
		if err := validatePlatforms(cfg.Platforms, version, allVersions); err != nil {
			return err
		}
		hashes, err := deps.Resolver.Hashes(ctx, addr, version, cfg.Platforms)
		if err != nil {
			return err
		}
		attrs = lockfile.ProviderAttrs{Version: version, Hashes: hashes, Constraints: cfg.Constraints}
	}

	if cfg.PrintBlock {
		fmt.Fprint(deps.Stdout, string(lockfile.RenderProviderBlock(addr.String(), attrs)))
		return nil
	}

	for _, path := range cfg.LockFiles {
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reading %s: %w", path, err)
		}
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

// validatePlatforms checks that every requested platform token is published
// for the chosen version.
func validatePlatforms(platforms []string, version string, allVersions []registry.Version) error {
	var ver *registry.Version
	for i := range allVersions {
		if allVersions[i].Version == version {
			ver = &allVersions[i]
			break
		}
	}
	if ver == nil {
		return nil // no platform info available; skip validation
	}
	avail := make(map[string]bool, len(ver.Platforms))
	for _, p := range ver.Platforms {
		avail[p.String()] = true
	}
	for _, plat := range platforms {
		if !avail[plat] {
			plats := make([]string, 0, len(ver.Platforms))
			for _, p := range ver.Platforms {
				plats = append(plats, p.String())
			}
			sort.Strings(plats)
			return fmt.Errorf("platform %q not available for %s %s; available: %s", plat, ver.Version, "", strings.Join(plats, ", "))
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
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	// Preserve the existing file's permissions.
	if info, statErr := os.Stat(path); statErr == nil {
		if chmodErr := f.Chmod(info.Mode()); chmodErr != nil {
			f.Close()
			return chmodErr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
