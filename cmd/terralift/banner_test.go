package main

import (
	"strings"
	"testing"
)

func TestBannerPlainHasNoANSI(t *testing.T) {
	plain := bannerText(false)
	if strings.Contains(plain, "\033[") {
		t.Errorf("plain banner contains ANSI escapes:\n%q", plain)
	}
	if !strings.Contains(plain, "T  E  R  R  A  L  I  F  T") {
		t.Errorf("banner missing wordmark:\n%s", plain)
	}
	// Log the plain render so `go test -run TestBanner -v` shows it.
	t.Logf("\n%s", plain)
}

func TestBannerColoredBalanced(t *testing.T) {
	c := bannerText(true)
	esc := strings.Count(c, "\033[")
	resets := strings.Count(c, cReset)
	if esc == 0 || resets == 0 {
		t.Fatal("colored banner has no ANSI styling")
	}
	// Each paint() emits one color-open + one reset; cReset ("\033[0m") also starts
	// with "\033[", so a fully balanced banner has total escapes == 2 * resets — i.e.
	// every color opened is closed, so styling never bleeds past the banner.
	if esc != 2*resets {
		t.Errorf("unbalanced styling: %d escapes, %d resets (want escapes == 2*resets)", esc, resets)
	}
}
