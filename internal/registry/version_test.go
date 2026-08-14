package registry

import "testing"

func TestSelectVersion(t *testing.T) {
	tests := []struct {
		name      string
		all       []string
		requested string
		want      string
	}{
		{"latest non-prerelease", []string{"4.0.0", "6.0.0", "5.0.0"}, "", "6.0.0"},
		{"prerelease excluded from latest", []string{"6.0.0-rc1", "5.0.0"}, "", "5.0.0"},
		{"explicit version", []string{"4.0.0", "5.0.0"}, "5.0.0", "5.0.0"},
		{"explicit prerelease allowed", []string{"6.0.0-rc1", "5.0.0"}, "6.0.0-rc1", "6.0.0-rc1"},
		{"leading v stripped", []string{"v2.0.0"}, "2.0.0", "2.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectVersion(tc.all, tc.requested)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSelectVersionErrors(t *testing.T) {
	if _, err := SelectVersion(nil, ""); err == nil {
		t.Error("expected error for empty list")
	}
	if _, err := SelectVersion([]string{"5.0.0"}, "9.9.9"); err == nil {
		t.Error("expected error for not-found version")
	}
}
