package main

import (
	"fmt"
	"os"
	"strings"
)

// Startup banner: a sun rising over a field that turns from brown to green — the
// tool's whole job (brownfield infra → greenfield IaC). ANSI 256-color; shown only
// on an interactive terminal (never in pipes/CI) and suppressible with --no-banner.
const (
	cReset = "\033[0m"
	cSun   = "\033[38;5;220m"  // gold — the sun disc
	cRay   = "\033[38;5;214m"  // orange — sun rays
	cBrown = "\033[38;5;130m"  // amber/brown — the brownfield
	cArrow = "\033[38;5;244m"  // grey — the transition
	cGreen = "\033[38;5;28m"   // green — the greenfield
	cGrass = "\033[38;5;82m"   // bright green — grass blades
	cWord  = "\033[1;38;5;82m" // bold bright green — the wordmark
)

func paint(color, s string, on bool) string {
	if !on {
		return s
	}
	return color + s + cReset
}

// bannerText renders the banner; color toggles ANSI escapes (off for tests/pipes).
func bannerText(color bool) string {
	sun := func(s string) string { return paint(cSun, s, color) }
	ray := func(s string) string { return paint(cRay, s, color) }
	brn := func(s string) string { return paint(cBrown, s, color) }
	arr := func(s string) string { return paint(cArrow, s, color) }
	grn := func(s string) string { return paint(cGreen, s, color) }
	grs := func(s string) string { return paint(cGrass, s, color) }
	wrd := func(s string) string { return paint(cWord, s, color) }

	lines := []string{
		`                     ` + ray(`\   |   /`),
		`                    ` + ray(`'  `) + sun(`.-~-.`) + ray(`  '`),
		`                  ` + ray(`── `) + sun(`(  ███  )`) + ray(` ──`),
		`                    ` + ray(`.  `) + sun(`'-~-'`) + ray(`  .`),
		`   ` + brn(`. ,;. ,.`) + `          ` + ray(`/   |   \`) + `       ` + grs(`\|/ \|/ \|/`),
		`   ` + brn(`; .,.'; .,`) + `     ` + arr(`══════════════▶`) + `    ` + grs(`\|/ \|/ \|/`),
		`   ` + brn(`▒▒▒▒▒▒▒▒▒▒`) + `                        ` + grn(`▓▓▓▓▓▓▓▓▓▓▓`),
		`   ` + brn(`▒▒▒▒▒▒▒▒▒▒`) + `                        ` + grn(`▓▓▓▓▓▓▓▓▓▓▓`),
		``,
		`             ` + wrd(`T  E  R  R  A  L  I  F  T`),
	}
	return strings.Join(lines, "\n") + "\n"
}

// printBanner writes the banner to stderr, only on an interactive terminal (so it
// never pollutes piped stdout or CI logs) unless suppressed.
func printBanner(noBanner bool) {
	if noBanner || !isTTY(os.Stderr) {
		return
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, bannerText(true))
	fmt.Fprintln(os.Stderr)
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
