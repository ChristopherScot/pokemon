package main

// Playthroughs: the real UI, rendered headless, driven by taps.

import (
	"image/png"
	"os"
	"testing"
)

// The register screen is what a new phone shows, and it must actually
// draw - a blank first screen is the worst possible first impression
// and the easiest thing to ship by accident.
func TestRegisterScreenRenders(t *testing.T) {
	h := newHarness(t)
	h.ui.screen = screenRegister

	img := h.frame()
	if !notBlank(img) {
		t.Fatal("the register screen rendered nothing")
	}
	if os.Getenv("SHOTS") != "" {
		f, _ := os.Create("/tmp/shot-register.png")
		png.Encode(f, img)
		f.Close()
	}
}
