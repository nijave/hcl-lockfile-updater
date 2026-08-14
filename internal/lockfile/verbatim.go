package lockfile

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type verbatimFile struct {
	Providers []verbatimProvider `hcl:"provider,block"`
}

type verbatimProvider struct {
	Address     string   `hcl:",label"`
	Version     string   `hcl:"version"`
	Constraints string   `hcl:"constraints,optional"`
	Hashes      []string `hcl:"hashes,optional"`
}

// DecodeVerbatimBlock decodes a file expected to hold exactly one provider
// block and returns its label and attributes.
func DecodeVerbatimBlock(data []byte) (string, ProviderAttrs, error) {
	file, diags := hclsyntax.ParseConfig(data, "block.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return "", ProviderAttrs{}, diags
	}
	var vf verbatimFile
	if diags := gohcl.DecodeBody(file.Body, nil, &vf); diags.HasErrors() {
		return "", ProviderAttrs{}, diags
	}
	if len(vf.Providers) == 0 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim block file contains no provider block")
	}
	if len(vf.Providers) > 1 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim block file must contain exactly one provider block, found %d", len(vf.Providers))
	}
	p := vf.Providers[0]
	if p.Version == "" {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim provider block is missing version")
	}
	if len(p.Hashes) == 0 {
		return "", ProviderAttrs{}, fmt.Errorf("verbatim provider block is missing hashes")
	}
	return p.Address, ProviderAttrs{Version: p.Version, Constraints: p.Constraints, Hashes: p.Hashes}, nil
}
