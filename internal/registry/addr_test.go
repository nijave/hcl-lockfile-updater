package registry

import "testing"

func TestParseProviderSource(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		registry   string
		wantAddr   ProviderAddr
		wantString string
	}{
		{"three parts uses embedded host", "registry.opentofu.org/hashicorp/aws", "", ProviderAddr{"registry.opentofu.org", "hashicorp", "aws"}, "registry.opentofu.org/hashicorp/aws"},
		{"two parts uses default host", "hashicorp/aws", "", ProviderAddr{DefaultRegistry, "hashicorp", "aws"}, "registry.opentofu.org/hashicorp/aws"},
		{"registry flag fills two parts", "hashicorp/aws", "registry.terraform.io", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
		{"registry flag overrides embedded host", "registry.opentofu.org/hashicorp/aws", "registry.terraform.io", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
		{"registry flag with scheme is normalized", "hashicorp/aws", "https://registry.terraform.io/", ProviderAddr{"registry.terraform.io", "hashicorp", "aws"}, "registry.terraform.io/hashicorp/aws"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseProviderSource(tc.raw, tc.registry)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantAddr {
				t.Errorf("got %+v, want %+v", got, tc.wantAddr)
			}
			if got.String() != tc.wantString {
				t.Errorf("String() = %q, want %q", got.String(), tc.wantString)
			}
		})
	}
}

func TestParseProviderSourceErrors(t *testing.T) {
	for _, raw := range []string{"", "aws", "a/b/c/d", "/hashicorp/aws", "hashicorp/"} {
		if _, err := ParseProviderSource(raw, ""); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}
