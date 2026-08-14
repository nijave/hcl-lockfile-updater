package lockfile

import "testing"

const cloudflareBlock = `provider "registry.opentofu.org/cloudflare/cloudflare" {
  version     = "5.22.0"
  constraints = "5.22.0"
  hashes = [
    "h1:EOm3pWL+XqsFNVhupvcedWz7XrYurk52G6ElTSJ+Fxk=",
    "zh:45f3b7c50254b1da1dc21e77e03cd1e931cab40fb75c7cba822a53ed54cd232e",
  ]
}
`

func TestDecodeVerbatimBlock(t *testing.T) {
	addr, attrs, err := DecodeVerbatimBlock([]byte(cloudflareBlock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "registry.opentofu.org/cloudflare/cloudflare" {
		t.Errorf("addr = %q", addr)
	}
	if attrs.Version != "5.22.0" {
		t.Errorf("version = %q", attrs.Version)
	}
	if attrs.Constraints != "5.22.0" {
		t.Errorf("constraints = %q", attrs.Constraints)
	}
	if len(attrs.Hashes) != 2 {
		t.Errorf("hashes = %v", attrs.Hashes)
	}
}

func TestDecodeVerbatimBlockErrors(t *testing.T) {
	cases := map[string]string{
		"no provider block":    `terraform { required_version = ">= 1.0" }`,
		"two provider blocks":  `provider "a/b/c" { version = "1.0.0" hashes = ["zh:1"] }
provider "x/y/z" { version = "1.0.0" hashes = ["zh:2"] }`,
		"missing version":      `provider "a/b/c" { hashes = ["zh:1"] }`,
		"missing hashes":       `provider "a/b/c" { version = "1.0.0" }`,
		"malformed hcl":        `provider "a/b/c" { version = `,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeVerbatimBlock([]byte(src)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
