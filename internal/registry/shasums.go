package registry

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// SHASUMLine is one parsed line of a sha256sum document.
type SHASUMLine struct {
	Hex      string
	Filename string
}

// ParseSHASUMSLines parses every non-empty line of a sha256sum document.
func ParseSHASUMSLines(body []byte) ([]SHASUMLine, error) {
	var out []SHASUMLine
	for i, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid SHASUMS line %d: expected digest and filename", i+1)
		}
		if !validSHA256Hex(fields[0]) {
			return nil, fmt.Errorf("invalid SHA-256 digest on SHASUMS line %d", i+1)
		}
		out = append(out, SHASUMLine{Hex: fields[0], Filename: fields[len(fields)-1]})
	}
	return out, nil
}

// ParseSHASUMS returns "zh:"-prefixed hashes for the given platforms only.
// A platform token like "linux_amd64" matches filenames containing "_linux_amd64".
func ParseSHASUMS(body []byte, platforms []string) ([]string, error) {
	want := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		want["_"+p] = true
	}
	var out []string
	lines, err := ParseSHASUMSLines(body)
	if err != nil {
		return nil, err
	}
	for _, ln := range lines {
		if matchesPlatform(ln.Filename, want) {
			out = append(out, "zh:"+ln.Hex)
		}
	}
	return out, nil
}

func validSHA256Hex(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func matchesPlatform(filename string, want map[string]bool) bool {
	for token := range want {
		// Require the token to be followed by ".zip" to avoid false positives
		// (e.g. _linux_arm matching _linux_arm64).
		if strings.Contains(filename, token+".zip") {
			return true
		}
	}
	return false
}
