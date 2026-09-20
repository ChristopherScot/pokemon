package main

import (
	"errors"
	"testing"
	"time"
)

// Joining is a battle transition, so it is testable as one.
//
// It used to live in a closure inside the HTTP handler, which meant
// the only way to exercise these rules was through the service - and
// the rules themselves (status, turn, version, the log line) sat in
// the transport layer where takeTurn's equivalents do not.
func TestJoinIsABattleTransition(t *testing.T) {
	dex, err := loadPokedex()
	if err != nil {
		t.Fatalf("pokedex: %v", err)
	}
	teamFor := func(names ...string) []*combatant {
		t.Helper()
		team, err := newCombatants(dex, names)
		if err != nil {
			t.Fatalf("building a team: %v", err)
		}
		return team
	}

	b := newBattle("b1", "ash", "tok-ash", teamFor("pikachu", "geodude", "bulbasaur"), time.Now())
	if b.status != "waiting" {
		t.Fatalf("a new battle is %q, want waiting", b.status)
	}
	if b.version != 1 {
		t.Errorf("a new battle is version %d, want 1", b.version)
	}
	if len(b.log) == 0 {
		t.Error("a new battle logged nothing; the lobby shows the opening line")
	}

	// The same trainer cannot join their own battle.
	if err := b.join("ash", "tok-ash", teamFor("charmander", "squirtle", "caterpie")); !errors.Is(err, errAlreadyIn) {
		t.Errorf("self-join = %v, want errAlreadyIn", err)
	}

	before := b.version
	if err := b.join("misty", "tok-misty", teamFor("charmander", "squirtle", "caterpie")); err != nil {
		t.Fatalf("join: %v", err)
	}
	if b.status != "active" {
		t.Errorf("after a join the battle is %q, want active", b.status)
	}
	if b.version <= before {
		t.Errorf("version did not advance on join: %d -> %d", before, b.version)
	}
	if b.turn != 0 {
		t.Errorf("turn = %d, want 0 - the opener moves first", b.turn)
	}

	// And a third trainer cannot join a full battle.
	if err := b.join("brock", "tok-brock", teamFor("onix", "pidgey", "rattata")); !errors.Is(err, errBattleFull) {
		t.Errorf("joining a full battle = %v, want errBattleFull", err)
	}
}
