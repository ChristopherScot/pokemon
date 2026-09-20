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

// A battle in progress: the screen a player spends the most time on,
// and the one where a layout mistake costs a turn.
func TestBattleScreenRenders(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	bc := testClient(t)
	h.ui.bc = bc
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")

	img := h.frame()
	if !notBlank(img) {
		t.Fatal("the battle screen rendered nothing")
	}
	if os.Getenv("SHOTS") != "" {
		f, _ := os.Create("/tmp/shot-battle.png")
		png.Encode(f, img)
		f.Close()
	}
}
