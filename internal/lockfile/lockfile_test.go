package lockfile

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderProviderBlock(t *testing.T) {
	out := RenderProviderBlock("registry.opentofu.org/hashicorp/aws", ProviderAttrs{
		Version:     "6.0.0",
		Constraints: "~> 6.0",
		Hashes:      []string{"h1:aaa=", "zh:bbbb"},
	}, FormatBlock)
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
	}, FormatBlock)
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

func TestMergeProviderBlockFormatBlockSplicesOnlyTarget(t *testing.T) {
	existing := []byte(`provider "a.example.com/x/keep" {
    version = "1.0.0"
  hashes = [ "h1:keep=" ]
}
provider "a.example.com/x/change" {
  version="2.0.0"
  hashes     = [
    "h1:old=",
  ]
}
`)
	out, err := MergeProviderBlock(existing, "a.example.com/x/change", ProviderAttrs{
		Version: "3.0.0",
		Hashes:  []string{"h1:new="},
	}, FormatBlock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	// The untouched block must survive byte-for-byte, ugly spacing included.
	keep := `provider "a.example.com/x/keep" {
    version = "1.0.0"
  hashes = [ "h1:keep=" ]
}
`
	if !strings.Contains(s, keep) {
		t.Errorf("untouched block not byte-preserved:\n%s", s)
	}
	// The updated block is formatter-normalized: aligned equals, single-line hashes.
	wantChange := `provider "a.example.com/x/change" {
  version = "3.0.0"
  hashes  = ["h1:new="]
}`
	if !strings.Contains(s, wantChange) {
		t.Errorf("target block not formatted:\n%s", s)
	}
}

func TestMergeProviderBlockFormatOffPlainWrites(t *testing.T) {
	existing := []byte(`provider "a.example.com/x/y" {
  version="2.0.0" # pinned by ci
  hashes     = [
    "h1:old=",
  ]
}
`)
	out, err := MergeProviderBlock(existing, "a.example.com/x/y", ProviderAttrs{
		Version:     "3.0.0",
		Constraints: "~> 3.0",
		Hashes:      []string{"h1:new=", "zh:nnnn"},
	}, FormatOff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `provider "a.example.com/x/y" {
  version = "3.0.0" # pinned by ci
  constraints = "~> 3.0"
  hashes = ["h1:new=", "zh:nnnn"]
}
`
	if string(out) != want {
		t.Errorf("plain write wrong.\nwant:\n%s\ngot:\n%s", want, string(out))
	}
}

func TestMergeProviderBlockFormatOffPreservesForeignSpacing(t *testing.T) {
	existing := []byte(`provider "a.example.com/x/y" {
    version     = "2.0.0"
  constraints   =   "~> 2.0"
}
`)
	out, err := MergeProviderBlock(existing, "a.example.com/x/y", ProviderAttrs{
		Version: "3.0.0",
	}, FormatOff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the version attribute changes; its neighbors keep their bytes,
	// including the odd indentation and padded equals.
	want := `provider "a.example.com/x/y" {
    version = "3.0.0"
  constraints   =   "~> 2.0"
}
`
	if string(out) != want {
		t.Errorf("plain write wrong.\nwant:\n%s\ngot:\n%s", want, string(out))
	}
}

func TestMergeProviderBlockFormatFileReformatsEverything(t *testing.T) {
	existing := []byte(`provider "a.example.com/x/keep" {
    version = "1.0.0"
}
provider "a.example.com/x/change" {
  version="2.0.0"
}
`)
	out, err := MergeProviderBlock(existing, "a.example.com/x/change", ProviderAttrs{
		Version: "3.0.0",
	}, FormatFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `provider "a.example.com/x/keep" {
  version = "1.0.0"
}
provider "a.example.com/x/change" {
  version = "3.0.0"
}
`
	if string(out) != want {
		t.Errorf("reformat wrong.\nwant:\n%s\ngot:\n%s", want, string(out))
	}
}

func TestMergeProviderBlockFormatModesAppendNew(t *testing.T) {
	existing := []byte("# just a comment\n")
	for _, format := range []Format{FormatBlock, FormatOff, FormatFile} {
		out, err := MergeProviderBlock(existing, "a.example.com/x/new", ProviderAttrs{
			Version: "1.0.0",
			Hashes:  []string{"h1:aaa="},
		}, format)
		if err != nil {
			t.Fatalf("format %v: unexpected error: %v", format, err)
		}
		s := string(out)
		if !strings.Contains(s, "# just a comment\n") {
			t.Errorf("format %v: comment lost:\n%s", format, s)
		}
		if !strings.Contains(s, `provider "a.example.com/x/new"`) || !strings.Contains(s, `"1.0.0"`) || !strings.Contains(s, "h1:aaa=") {
			t.Errorf("format %v: appended block missing content:\n%s", format, s)
		}
		if !strings.HasSuffix(s, "}\n") {
			t.Errorf("format %v: output must end with a newline after the block:\n%s", format, s)
		}
	}
}

func TestRenderProviderBlockPlain(t *testing.T) {
	out := RenderProviderBlock("a.example.com/x/y", ProviderAttrs{
		Version:     "1.0.0",
		Constraints: "~> 1.0",
		Hashes:      []string{"h1:aaa=", "zh:bbb"},
	}, FormatOff)
	want := `provider "a.example.com/x/y" {
  version = "1.0.0"
  constraints = "~> 1.0"
  hashes = ["h1:aaa=", "zh:bbb"]
}
`
	if string(out) != want {
		t.Errorf("plain render wrong.\nwant:\n%s\ngot:\n%s", want, string(out))
	}
	// Formatted variants (the default render paths) go through hclwrite.
	for _, format := range []Format{FormatBlock, FormatFile} {
		got := RenderProviderBlock("a.example.com/x/y", ProviderAttrs{
			Version: "1.0.0",
			Hashes:  []string{"h1:aaa="},
		}, format)
		if bytes.Equal(got, out[:len(out)]) {
			t.Errorf("format %v: expected hclwrite rendering, got plain", format)
		}
		if !strings.Contains(string(got), `hashes  = ["h1:aaa="]`) {
			t.Errorf("format %v: expected aligned hclwrite output:\n%s", format, string(got))
		}
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
	}, FormatBlock)
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
