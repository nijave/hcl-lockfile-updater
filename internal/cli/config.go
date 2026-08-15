package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/nijave/hcl-lockfile-updater/internal/lockfile"
	"github.com/spf13/pflag"
)

// Mode selects the input shape.
type Mode int

const (
	ModeLookup Mode = iota
	ModeVerbatim
)

// Config is the parsed CLI configuration.
type Config struct {
	Mode        Mode
	BlockFile   string // verbatim mode
	ProviderRaw string // lookup mode raw address
	Version     string
	Platforms   []string
	Constraints string
	Registry    string
	PrintBlock  bool
	LockFiles   []string
	// FormatBlockMode: format the provider block the tool writes (default).
	// FormatOff: no formatter pass. FormatFile: reformat the entire file.
	Format lockfile.Format
	// SkipMissing: only update lock files that already contain a matching
	// provider block. Missing files and files without the block are skipped.
	SkipMissing bool
}

// ParseArgs parses argv (without the program name) into a Config.
func ParseArgs(args []string) (Config, error) {
	fs := pflag.NewFlagSet("hcl-lockfile-updater", pflag.ContinueOnError)
	var version, constraints, registry, blockFile string
	var platforms []string
	var printBlock, format, reformat, skipMissing bool

	fs.StringVar(&version, "version", "", "exact version to pin")
	fs.StringArrayVar(&platforms, "platform", nil, "target platform os_arch (repeatable)")
	fs.StringVar(&constraints, "constraints", "", "set or replace the constraints attribute")
	fs.StringVar(&registry, "registry", "", "registry host to query")
	fs.StringVar(&blockFile, "block-file", "", "file containing one provider block (verbatim mode)")
	fs.BoolVar(&printBlock, "print-block", false, "print the resolved provider block to stdout")
	fs.BoolVar(&format, "format", true, "run written provider block bytes through the hcl formatter")
	fs.BoolVar(&reformat, "reformat", false, "reformat the entire lock file (overrides --format)")
	fs.BoolVar(&skipMissing, "skip-missing", false, "only update lock files that already contain the provider block")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	rest := fs.Args()
	cfg := Config{
		Version: version, Platforms: platforms, Constraints: constraints,
		Registry: registry, BlockFile: blockFile, PrintBlock: printBlock,
		SkipMissing: skipMissing,
	}
	// --reformat takes precedence over --format.
	switch {
	case reformat:
		cfg.Format = lockfile.FormatFile
	case format:
		cfg.Format = lockfile.FormatBlock
	default:
		cfg.Format = lockfile.FormatOff
	}

	if blockFile != "" {
		cfg.Mode = ModeVerbatim
		cfg.LockFiles = rest
	} else {
		cfg.Mode = ModeLookup
		if len(rest) == 0 {
			return Config{}, fmt.Errorf("missing provider source address (or use --block-file)")
		}
		cfg.ProviderRaw = rest[0]
		cfg.LockFiles = rest[1:]
		if len(cfg.Platforms) == 0 {
			cfg.Platforms = []string{runtime.GOOS + "_" + runtime.GOARCH}
		}
		for _, p := range cfg.Platforms {
			if !validPlatform(p) {
				return Config{}, fmt.Errorf("invalid platform %q: expected os_arch", p)
			}
		}
	}

	if !cfg.PrintBlock && len(cfg.LockFiles) == 0 {
		return Config{}, fmt.Errorf("at least one lock file is required (or use --print-block)")
	}
	return cfg, nil
}

func validPlatform(p string) bool {
	parts := strings.SplitN(p, "_", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
