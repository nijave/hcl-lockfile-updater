package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nijave/hcl-lockfile-updater/internal/cli"
	"github.com/nijave/hcl-lockfile-updater/internal/registry"
	"github.com/spf13/pflag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := cli.ParseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			// pflag already printed usage.
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	client := registry.NewClient(nil)
	deps := cli.Deps{
		Lister:   client,
		Resolver: registry.NewResolver(client),
		Stdout:   os.Stdout,
	}
	if err := cli.Run(ctx, cfg, deps); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
