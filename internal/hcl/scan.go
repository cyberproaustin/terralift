package hcl

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// scanResourceLabelRe matches a `resource "type" "name"` header at the start of
// a line. It intentionally does NOT require a trailing brace, so it matches both
// `resource "t" "n" {` and a formatter-split `resource "t" "n"` header. Every
// provider's export used an identical copy of this regex.
var scanResourceLabelRe = regexp.MustCompile(`(?m)^resource\s+"([^"]+)"\s+"([^"]+)"`)

// ScanAddrs returns the set of `type.name` resource addresses declared in the
// given HCL text. This is the shared replacement for the per-provider
// scanResourceAddrs/scanGeneratedAddrs/declaredAddrs helpers, which each read a
// file, applied the same regex, and returned the same set.
func ScanAddrs(hclText string) map[string]bool {
	out := map[string]bool{}
	for _, m := range scanResourceLabelRe.FindAllStringSubmatch(hclText, -1) {
		out[m[1]+"."+m[2]] = true
	}
	return out
}

// ScanAddrsFile returns ScanAddrs of the file at path, or an empty set if the
// file cannot be read (the honest "nothing generated" case a caller relies on).
func ScanAddrsFile(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	return ScanAddrs(string(data))
}

// ScanAddrsDir unions ScanAddrs across every .tf file in dir (used where the
// generator spreads resources over multiple files, e.g. aztfexport's output).
func ScanAddrsDir(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		for a := range ScanAddrsFile(filepath.Join(dir, e.Name())) {
			out[a] = true
		}
	}
	return out
}
