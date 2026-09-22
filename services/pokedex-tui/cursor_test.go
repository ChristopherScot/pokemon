package main

import (
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

// serverMon is a Pokemon shaped the way the SERVER publishes one.
//
// The existing mon() helper leaves CanAct and CanBeTargeted unset, so
// battleclient falls back to !Fainted and the tests exercise a
// compatibility path a live server never produces. That is why the
// target cursor could be completely broken against production while
// TestCursorSkipsFaintedPokemon passed.
//
// The server's projection is:
//
//	CanAct:        sideActive && c.canAct()
//	CanBeTargeted: !sideActive && c.canBeTargeted()
//
// so the two are mutually exclusive - every Pokemon on the opposing
// side has CanAct false, always.
func serverMon(name string, hp int, fainted, sideActive bool) api.BattlePokemon {
	p := mon(name, hp, 100, fainted)
	p.SetCanAct(api.NewOptBool(sideActive && !fainted))
	p.SetCanBeTargeted(api.NewOptBool(!sideActive && !fainted))
	return p
}

// You can attack the opponent's second and third Pokemon.
//
// The target cursor was gated on CanAct, which is false for every
// Pokemon on the opposing side by construction - so nextPick found
// nothing selectable and returned the index unchanged. The cursor sat
// on their first Pokemon and could not be moved, for the whole game.
func TestTheTargetCursorCanReachEveryLiveOpponent(t *testing.T) {
	theirs := []api.BattlePokemon{
		serverMon("onix", 95, false, false),
		serverMon("gengar", 120, false, false),
		serverMon("mew", 100, false, false),
	}

	seen := map[int]bool{0: true}
	at := 0
	for i := 0; i < len(theirs); i++ {
		at = nextPick(theirs, at, battleclient.CanBeTargeted)
		seen[at] = true
	}
	if len(seen) != len(theirs) {
		t.Errorf("the target cursor reached %d of %d opponents; gating it on "+
			"CanAct freezes it on the first, because the server publishes "+
			"CanAct false for the whole opposing side", len(seen), len(theirs))
	}
}

// And it still skips a fainted one.
func TestTheTargetCursorSkipsTheFainted(t *testing.T) {
	theirs := []api.BattlePokemon{
		serverMon("onix", 95, false, false),
		serverMon("gengar", 0, true, false),
		serverMon("mew", 100, false, false),
	}
	if got := nextPick(theirs, 0, battleclient.CanBeTargeted); got != 2 {
		t.Errorf("target cursor moved to %d, want 2 - slot 1 has fainted", got)
	}
}

// The attacker cursor keeps its own rule: on YOUR side, CanAct is the
// one that is true.
func TestTheAttackerCursorUsesCanAct(t *testing.T) {
	mine := []api.BattlePokemon{
		serverMon("pikachu", 0, true, true),
		serverMon("charmander", 99, false, true),
	}
	if got := nextPick(mine, 0, battleclient.CanAct); got != 1 {
		t.Errorf("attacker cursor moved to %d, want 1 - slot 0 has fainted", got)
	}
	// And gating the ATTACKER on CanBeTargeted would freeze it the same
	// way, which is the mirror of the bug.
	if got := nextPick(mine, 0, battleclient.CanBeTargeted); got != 0 {
		t.Logf("sanity: CanBeTargeted is false on your own side, so it would "+
			"freeze the attacker cursor at %d", got)
	}
}

// A row's colour and its glyph agree about what happened.
//
// The TUI re-derived "super effective" / "resisted" / "no effect"
// from the raw effectiveness float at four separate sites, while the
// glyph came from battletext.EventIcon - which classifies via
// Classify, and Classify ranks Fainted ABOVE effectiveness. So a
// killing super-effective blow showed the skull icon and
// super-effective styling in the same row, describing two different
// events. The four copies had also drifted among themselves: two
// guarded the resisted case with e > 0 && e < 1, two used a bare
// e < 1.
func TestAKillingBlowIsStyledAsAFaintNotAsSuperEffective(t *testing.T) {
	ev := api.BattleEvent{
		Text:          "Charmander used Ember on Bulbasaur!",
		Target:        api.NewOptString("bulbasaur"),
		Damage:        api.NewOptInt(60),
		Effectiveness: api.NewOptFloat64(2),
		Fainted:       api.NewOptBool(true),
	}
	if got, want := battletext.Classify(ev), battletext.EventFainted; got != want {
		t.Fatalf("Classify = %v, want %v - the rest of this test assumes "+
			"fainting outranks effectiveness", got, want)
	}

	// Style and glyph must come from that same answer.
	faint := impactStyle(battletext.EventFainted)
	super := impactStyle(battletext.EventSuperEffective)
	if faint.Render("x") == super.Render("x") {
		t.Error("a faint and a super-effective hit render identically; the " +
			"skull icon and the colour would be describing different events")
	}
}

// And the resisted boundary is one rule, not four.
func TestResistedIsClassifiedOnceForEveryPartOfTheRow(t *testing.T) {
	for _, e := range []float64{0.25, 0.5} {
		ev := api.BattleEvent{
			Text: "hit", Target: api.NewOptString("x"),
			Damage: api.NewOptInt(3), Effectiveness: api.NewOptFloat64(e),
		}
		if got := battletext.Classify(ev); got != battletext.EventResisted {
			t.Errorf("Classify(%v) = %v, want EventResisted", e, got)
		}
	}
}
