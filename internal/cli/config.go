package cli

import (
	"fmt"
	"runtime"
	"strings"

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
}

// ParseArgs parses argv (without the program name) into a Config.
func ParseArgs(args []string) (Config, error) {
	fs := pflag.NewFlagSet("hcl-lockfile-updater", pflag.ContinueOnError)
	var version, constraints, registry, blockFile string
	var platforms []string
	var printBlock bool

	fs.StringVar(&version, "version", "", "exact version to pin")
	fs.StringArrayVar(&platforms, "platform", nil, "target platform os_arch (repeatable)")
	fs.StringVar(&constraints, "constraints", "", "set or replace the constraints attribute")
	fs.StringVar(&registry, "registry", "", "registry host to query")
	fs.StringVar(&blockFile, "block-file", "", "file containing one provider block (verbatim mode)")
	fs.BoolVar(&printBlock, "print-block", false, "print the resolved provider block to stdout")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	rest := fs.Args()
	cfg := Config{
		Version: version, Platforms: platforms, Constraints: constraints,
		Registry: registry, BlockFile: blockFile, PrintBlock: printBlock,
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
