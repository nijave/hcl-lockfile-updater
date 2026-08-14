package registry

import (
	"reflect"
	"testing"
)

func TestParseSHASUMSLines(t *testing.T) {
	body := []byte("aaaa  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		"bbbb  terraform-provider-aws_6.0.0_darwin_arm64.zip\n" +
		"\n" +
		"cccc  terraform-provider-aws_6.0.0_windows_amd64.zip\n")
	got := ParseSHASUMSLines(body)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(got), got)
	}
	if got[0].Hex != "aaaa" || got[0].Filename != "terraform-provider-aws_6.0.0_linux_amd64.zip" {
		t.Errorf("first line wrong: %+v", got[0])
	}
}

func TestParseSHASUMS(t *testing.T) {
	body := []byte("aaaa  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		"bbbb  terraform-provider-aws_6.0.0_darwin_arm64.zip\n")
	got := ParseSHASUMS(body, []string{"linux_amd64"})
	want := []string{"zh:aaaa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got2 := ParseSHASUMS(body, []string{"linux_amd64", "darwin_arm64"})
	want2 := []string{"zh:aaaa", "zh:bbbb"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("got %v, want %v", got2, want2)
	}
}
