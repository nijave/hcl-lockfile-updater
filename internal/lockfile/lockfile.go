package lockfile

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// ProviderAttrs holds the attributes the tool may set on a provider block.
// A zero-valued Constraints or empty Hashes slice means "do not touch".
type ProviderAttrs struct {
	Version     string
	Constraints string
	Hashes      []string
}

func applyAttrs(body *hclwrite.Body, attrs ProviderAttrs) {
	if attrs.Version != "" {
		body.SetAttributeValue("version", cty.StringVal(attrs.Version))
	}
	if attrs.Constraints != "" {
		body.SetAttributeValue("constraints", cty.StringVal(attrs.Constraints))
	}
	if len(attrs.Hashes) > 0 {
		vals := make([]cty.Value, 0, len(attrs.Hashes))
		for _, h := range attrs.Hashes {
			vals = append(vals, cty.StringVal(h))
		}
		body.SetAttributeRaw("hashes", hclwrite.TokensForValue(cty.ListVal(vals)))
	}
}

// RenderProviderBlock renders a single standalone provider {} block.
func RenderProviderBlock(addr string, attrs ProviderAttrs) []byte {
	f := hclwrite.NewEmptyFile()
	b := f.Body().AppendNewBlock("provider", []string{addr})
	applyAttrs(b.Body(), attrs)
	return f.Bytes()
}

// MergeProviderBlock merges attrs into the provider block for addr within data.
// Attributes not present in attrs are preserved. If no matching block exists, a
// new one is appended. Empty data starts a fresh file.
func MergeProviderBlock(data []byte, addr string, attrs ProviderAttrs) ([]byte, error) {
	f, diags := hclwrite.ParseConfig(data, ".terraform.lock.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	block := findProviderBlock(f.Body(), addr)
	if block == nil {
		block = f.Body().AppendNewBlock("provider", []string{addr})
	}
	applyAttrs(block.Body(), attrs)
	out := f.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

func findProviderBlock(body *hclwrite.Body, addr string) *hclwrite.Block {
	for _, b := range body.Blocks() {
		if b.Type() == "provider" && len(b.Labels()) == 1 && b.Labels()[0] == addr {
			return b
		}
	}
	return nil
}
