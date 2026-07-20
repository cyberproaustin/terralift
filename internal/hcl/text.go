package hcl

import "strings"

// Tail returns the last n lines of s (trailing newline trimmed first), for
// truncating tool output in log/diagnostic messages. Shared replacement for the
// identical `tail` helper each provider carried.
func Tail(s string, n int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "\n")
}
