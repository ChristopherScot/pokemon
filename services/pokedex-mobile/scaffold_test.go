package main

// Tests for the scaffold, which run anywhere.
//
// Nothing here opens a window. Gio's layout needs a frame and a CI
// runner has no display, so anything drawing is an integration test
// with a device or an emulator in it - out of scope for `go test`.
// What IS testable is the logic in state.go, which is why it lives
// there rather than inside layout().

import (
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestSummariseUsesTheSharedWording(t *testing.T) {
	// Not a golden string: the point is that the wording comes from
	// battletext, so asserting it here would just duplicate that
	// package's tests and drift from them.
	p := api.BattlePokemon{Name: "pikachu", Types: []string{"electric"}}
	got := summarise(p)
	if !strings.HasPrefix(got, "pikachu") {
		t.Errorf("summarise() = %q, want it to start with the name", got)
	}

	// A condition the CLI and TUI would show must show here too.
	p.Confused = api.NewOptBool(true)
	if withCond := summarise(p); withCond == got {
		t.Error("a confused Pokemon rendered identically to a healthy one; battletext.Conditions is not reaching the UI")
	}
}

// The version is stamped by CI with -ldflags. A released build
// reporting "dev" means that flag was dropped, which is invisible
// until someone asks a user what version they are running.
func TestVersionHasADefault(t *testing.T) {
	if version == "" {
		t.Error("version is empty; the ldflags default was removed")
	}
}
