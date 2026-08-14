package registry

import "strings"

// SHASUMLine is one parsed line of a sha256sum document.
type SHASUMLine struct {
	Hex      string
	Filename string
}

// ParseSHASUMSLines parses every non-empty line of a sha256sum document.
func ParseSHASUMSLines(body []byte) []SHASUMLine {
	var out []SHASUMLine
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, SHASUMLine{Hex: fields[0], Filename: fields[len(fields)-1]})
	}
	return out
}

// ParseSHASUMS returns "zh:"-prefixed hashes for the given platforms only.
// A platform token like "linux_amd64" matches filenames containing "_linux_amd64".
func ParseSHASUMS(body []byte, platforms []string) []string {
	want := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		want["_"+p] = true
	}
	var out []string
	for _, ln := range ParseSHASUMSLines(body) {
		if matchesPlatform(ln.Filename, want) {
			out = append(out, "zh:"+ln.Hex)
		}
	}
	return out
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
