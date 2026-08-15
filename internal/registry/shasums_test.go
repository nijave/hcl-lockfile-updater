package registry

import (
	"reflect"
	"strings"
	"testing"
)

const (
	testDigestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDigestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testDigestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestParseSHASUMSLines(t *testing.T) {
	body := []byte(testDigestA + "  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		testDigestB + "  terraform-provider-aws_6.0.0_darwin_arm64.zip\n" +
		"\n" +
		testDigestC + "  terraform-provider-aws_6.0.0_windows_amd64.zip\n")
	got, err := ParseSHASUMSLines(body)
	if err != nil {
		t.Fatalf("ParseSHASUMSLines: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(got), got)
	}
	if got[0].Hex != testDigestA || got[0].Filename != "terraform-provider-aws_6.0.0_linux_amd64.zip" {
		t.Errorf("first line wrong: %+v", got[0])
	}
}

func TestParseSHASUMS(t *testing.T) {
	body := []byte(testDigestA + "  terraform-provider-aws_6.0.0_linux_amd64.zip\n" +
		testDigestB + "  terraform-provider-aws_6.0.0_darwin_arm64.zip\n")
	got, err := ParseSHASUMS(body, []string{"linux_amd64"})
	if err != nil {
		t.Fatalf("ParseSHASUMS: %v", err)
	}
	want := []string{"zh:" + testDigestA}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got2, err := ParseSHASUMS(body, []string{"linux_amd64", "darwin_arm64"})
	if err != nil {
		t.Fatalf("ParseSHASUMS: %v", err)
	}
	want2 := []string{"zh:" + testDigestA, "zh:" + testDigestB}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("got %v, want %v", got2, want2)
	}
}

func TestParseSHASUMSNoFalsePositive(t *testing.T) {
	// linux_arm must NOT match linux_arm64 filenames.
	body := []byte(testDigestA + "  terraform-provider-aws_6.0.0_linux_arm64.zip\n" +
		testDigestB + "  terraform-provider-aws_6.0.0_linux_arm.zip\n")
	gotArm, err := ParseSHASUMS(body, []string{"linux_arm"})
	if err != nil {
		t.Fatalf("ParseSHASUMS: %v", err)
	}
	if len(gotArm) != 1 || gotArm[0] != "zh:"+testDigestB {
		t.Errorf("linux_arm matched wrong entries: %v", gotArm)
	}
	gotArm64, err := ParseSHASUMS(body, []string{"linux_arm64"})
	if err != nil {
		t.Fatalf("ParseSHASUMS: %v", err)
	}
	if len(gotArm64) != 1 || gotArm64[0] != "zh:"+testDigestA {
		t.Errorf("linux_arm64 matched wrong entries: %v", gotArm64)
	}
}

func TestParseSHASUMSLinesRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"missing filename": testDigestA,
		"short digest":     "aaaa  provider_linux_amd64.zip",
		"uppercase digest": strings.ToUpper(testDigestA) + "  provider_linux_amd64.zip",
		"non-hex digest":   strings.Repeat("g", 64) + "  provider_linux_amd64.zip",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSHASUMSLines([]byte(body)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
