package main

// The app's logic, kept out of main.go on purpose.
//
// Anything that touches layout.Context needs a Gio frame to run, and a
// CI runner has no display - so a test of the UI is an integration
// test with a window in it. Logic that lives here is a plain function
// over plain values, and `go test ./...` covers it on any machine.
//
// The seam to aim for: layout() reads state and draws it, and every
// decision about WHAT to draw is a function here.

import (
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

// summarise renders one Pokemon the way the CLI and the TUI render it.
//
// The wording comes from battletext rather than from here, so three
// clients narrate a battle identically. A phone showing "paralyzed"
// where the terminal shows "PAR" is the kind of drift that only turns
// up when someone has both open.
func summarise(p api.BattlePokemon) string {
	parts := []string{p.Name}
	if s := battletext.StageLabel(p); s != "" {
		parts = append(parts, s)
	}
	if c := battletext.Conditions(p); len(c) > 0 {
		parts = append(parts, strings.Join(c, " "))
	}
	return strings.Join(parts, "  ")
}
