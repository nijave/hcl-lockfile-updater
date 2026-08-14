package lockfile

import (
	"strings"
	"testing"
)

func TestRenderProviderBlock(t *testing.T) {
	out := RenderProviderBlock("registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version:     "6.0.0",
		Constraints: "~> 6.0",
		Hashes:      []string{"h1:aaa=", "zh:bbbb"},
	})
	s := string(out)
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/aws"`) {
		t.Errorf("missing provider header: %s", s)
	}
	if !strings.Contains(s, "version") || !strings.Contains(s, `"6.0.0"`) {
		t.Errorf("missing version: %s", s)
	}
	if !strings.Contains(s, "h1:aaa=") || !strings.Contains(s, "zh:bbbb") {
		t.Errorf("missing hashes: %s", s)
	}
}

func TestMergeProviderBlockPreservesUnspecified(t *testing.T) {
	existing := []byte(`provider "registry.opentofu.org/hashicorp/aws" {
  version     = "5.0.0"
  constraints = "~> 5.0"
  hashes = [
    "h1:old=",
  ]
}
`)
	out, err := MergeProviderBlock(existing, "registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version: "6.0.0",
		Hashes:  []string{"h1:new="},
		// Constraints intentionally empty: must be preserved.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"6.0.0"`) || strings.Contains(s, `"5.0.0"`) {
		t.Errorf("version not updated: %s", s)
	}
	if !strings.Contains(s, `constraints = "~> 5.0"`) {
		t.Errorf("existing constraints not preserved: %s", s)
	}
	if !strings.Contains(s, "h1:new=") || strings.Contains(s, "h1:old=") {
		t.Errorf("hashes not replaced: %s", s)
	}
}

func TestMergeProviderBlockAppendsNewAndPreservesOthers(t *testing.T) {
	existing := []byte(`# top comment
provider "registry.opentofu.org/hashicorp/random" {
  version = "3.0.0"
  hashes  = ["zh:rrr"]
}
`)
	out, err := MergeProviderBlock(existing, "registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version: "6.0.0",
		Hashes:  []string{"h1:new="},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "# top comment") {
		t.Errorf("comment not preserved: %s", s)
	}
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/random"`) {
		t.Errorf("other provider not preserved: %s", s)
	}
	if !strings.Contains(s, `provider "registry.opentofu.org/hashicorp/aws"`) {
		t.Errorf("new provider not appended: %s", s)
	}
}
