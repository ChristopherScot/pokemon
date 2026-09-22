package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// A forgotten trainer leads back to the register screen.
//
// The server drops trainers when it restarts, and the status told the
// player to "register again" - but screenRegister was set in exactly
// one place, first launch, and ClearIdentity was never called
// anywhere in this app. So the message named an action the app could
// not perform, and on a phone the only way out was reinstalling.
func TestAStaleIdentitySendsYouBackToRegister(t *testing.T) {
	a := newUI(nil)
	a.screen = screenBrowse
	a.id = battleclient.Identity{Name: "ash", Token: "gone"}

	a.apply(result{kind: resBattle, err: fmt.Errorf("getting battle: %w",
		battleclient.ErrStaleIdentity)})

	if a.screen != screenRegister {
		t.Errorf("screen is %v after a stale identity, want screenRegister - the "+
			"status tells the player to register again, so there has to be a way",
			a.screen)
	}
	if a.id.Token != "" {
		t.Error("the dead token was kept; the next request repeats the failure")
	}
	if a.status == "" {
		t.Error("no status explaining why the player is back at register")
	}
}

// An ordinary failure must NOT throw the player out.
func TestATimeoutDoesNotSendYouBackToRegister(t *testing.T) {
	a := newUI(nil)
	a.screen = screenBrowse
	a.id = battleclient.Identity{Name: "ash", Token: "good"}

	a.apply(result{kind: resBattle, err: errors.New("context deadline exceeded")})

	if a.screen == screenRegister {
		t.Error("a timeout sent the player back to register and discarded their " +
			"identity; only a stale identity should do that")
	}
	if a.id.Token != "good" {
		t.Error("a timeout discarded a perfectly good token")
	}
}

// The watch cursor is a version, not a log length.
//
// Watch compares b.Version > seen. lastSeen was assigned
// len(battle.Log), a different quantity - at battle start len(Log) is
// 0 while Version is already 1, so the long poll returned instantly,
// keepWatching re-armed it on the next frame, and the phone hammered
// GET /battles/{id} at frame rate for the whole of the opponent's
// turn.
func TestTheWatchCursorIsTheVersion(t *testing.T) {
	a := newUI(nil)
	b := &api.Battle{
		ID: "abc", Status: api.BattleStatusActive, Version: 7,
		Turn: api.NewOptString("gary"),
		Log: []api.BattleEvent{
			{Text: "one"}, {Text: "two"}, {Text: "three"},
		},
	}
	a.apply(result{kind: resBattle, battle: b})

	if a.lastVersion != 7 {
		t.Errorf("watch cursor = %d, want 7 (the version); it was being set to "+
			"len(Log) = %d, which Watch compares against Version",
			a.lastVersion, len(b.Log))
	}
}

// A monogram survives whatever the server sends.
func TestTheMonogramSurvivesAnyName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"pikachu", "P"},
		{"", "?"},      // panicked: slice bounds out of range
		{"Ωmega", "Ω"}, // rendered as a replacement box
		{"élodie", "É"},
	} {
		if got := initial(tc.in); got != tc.want {
			t.Errorf("initial(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
