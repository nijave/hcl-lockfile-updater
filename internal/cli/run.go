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
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
