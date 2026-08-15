package cli

import (
	"errors"
	"runtime"
	"testing"

	"github.com/nijave/hcl-lockfile-updater/internal/lockfile"
	"github.com/spf13/pflag"
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

// main exits 0 on pflag.ErrHelp, so ParseArgs must keep reporting help
// through that sentinel rather than wrapping or replacing it.
func TestParseArgsHelpReportsErrHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		if _, err := ParseArgs(args); !errors.Is(err, pflag.ErrHelp) {
			t.Errorf("ParseArgs(%v) error = %v, want pflag.ErrHelp", args, err)
		}
	}
}

func TestParseArgsPrintBlockNoFiles(t *testing.T) {
	if _, err := ParseArgs([]string{"hashicorp/aws", "--print-block"}); err != nil {
		t.Fatalf("print-block should allow no files: %v", err)
	}
}

func TestParseArgsFormatDefaults(t *testing.T) {
	cfg, err := ParseArgs([]string{"hashicorp/aws", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Format != lockfile.FormatBlock {
		t.Errorf("Format default = %v, want FormatBlock", cfg.Format)
	}
}

func TestParseArgsFormatOff(t *testing.T) {
	cfg, err := ParseArgs([]string{"hashicorp/aws", "--format=false", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Format != lockfile.FormatOff {
		t.Errorf("Format = %v, want FormatOff", cfg.Format)
	}
}

func TestParseArgsReformatOverridesFormat(t *testing.T) {
	// --reformat wins even when --format is explicitly disabled.
	cfg, err := ParseArgs([]string{"hashicorp/aws", "--format=false", "--reformat", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Format != lockfile.FormatFile {
		t.Errorf("Format = %v, want FormatFile (--reformat takes precedence)", cfg.Format)
	}
}

func TestParseArgsReformatAlone(t *testing.T) {
	cfg, err := ParseArgs([]string{"hashicorp/aws", "--reformat", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Format != lockfile.FormatFile {
		t.Errorf("Format = %v, want FormatFile", cfg.Format)
	}
}

func TestParseArgsSkipMissing(t *testing.T) {
	cfg, err := ParseArgs([]string{"hashicorp/aws", "--skip-missing", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SkipMissing {
		t.Errorf("SkipMissing = false, want true")
	}
	cfg, err = ParseArgs([]string{"hashicorp/aws", "a.lock.hcl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SkipMissing {
		t.Errorf("SkipMissing default = true, want false")
	}
}
