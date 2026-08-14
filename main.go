package main

import (
	"context"
	"fmt"
	"os"

	"github.com/nijave/terragrunt-providers-pin/internal/cli"
	"github.com/nijave/terragrunt-providers-pin/internal/registry"
)

func main() {
	cfg, err := cli.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	client := registry.NewClient(nil)
	deps := cli.Deps{
		Lister:   client,
		Resolver: registry.NewResolver(client),
		Stdout:   os.Stdout,
	}
	if err := cli.Run(context.Background(), cfg, deps); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
