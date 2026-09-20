package main

import (
	"testing"

	"gioui.org/io/key"
)

// Back must go somewhere from every non-root screen. Unhandled, it
// kills the app mid-battle.
func TestBackNavigatesRatherThanExiting(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)

	// From a sub-screen, back returns to the Pokedex.
	h.ui.screen = screenLobby
	if consumed := h.ui.handleBack(h.gtx()); !consumed {
		t.Error("back from the lobby was not consumed; Android would close the app")
	}
	if h.ui.screen != screenBrowse {
		t.Errorf("back from the lobby went to %v, want browse", h.ui.screen)
	}

	// From the root it is NOT consumed, so Android closes the app -
	// an app you cannot back out of is worse than one that exits.
	h.ui.screen = screenBrowse
	if consumed := h.ui.handleBack(h.gtx()); consumed {
		t.Error("back from the Pokedex was consumed; the app can never be closed")
	}
}

// A live battle should not be abandoned by one stray gesture.
func TestBackConfirmsBeforeLeavingABattle(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")

	h.ui.handleBack(h.gtx())
	if h.ui.screen != screenBattle {
		t.Fatal("one back press left a live battle without confirming")
	}
	if h.ui.status == "" {
		t.Error("nothing told the player a second press would leave")
	}
	h.ui.handleBack(h.gtx())
	if h.ui.screen == screenBattle {
		t.Error("a second back press did not leave the battle")
	}
}

// Mid-turn, back undoes the selection instead of leaving, so the
// gesture agrees with the on-screen Back button.
func TestBackUndoesASelectionFirst(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")
	h.ui.sel.haveAttacker = true

	h.ui.handleBack(h.gtx())
	if h.ui.sel.haveAttacker {
		t.Error("back did not undo the attacker selection")
	}
	if h.ui.screen != screenBattle {
		t.Error("back left the battle instead of undoing the selection")
	}
}

var _ = key.NameBack
