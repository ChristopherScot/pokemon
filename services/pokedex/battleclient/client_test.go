package battleclient

import (
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func battle(status api.BattleStatus, turn string, sides ...string) *api.Battle {
	b := &api.Battle{Status: status}
	if turn != "" {
		b.Turn = api.NewOptString(turn)
	}
	for _, s := range sides {
		b.Sides = append(b.Sides, api.Side{Trainer: s})
	}
	return b
}

// MyTurn exists because every caller needs it and comparing the wrong
// field is an easy mistake: a client that thinks it is always its turn
// spams 409s, and one that never thinks so hangs forever.
func TestMyTurn(t *testing.T) {
	c := &Client{Name: "ash"}
	for _, tc := range []struct {
		name string
		b    *api.Battle
		want bool
	}{
		{"my move", battle(api.BattleStatusActive, "ash", "ash", "gary"), true},
		{"their move", battle(api.BattleStatusActive, "gary", "ash", "gary"), false},
		// Waiting and finished have no turn; treating an absent turn as
		// "mine" would make a client attack into a battle that is over.
		{"still waiting", battle(api.BattleStatusWaiting, "", "ash"), false},
		{"finished", battle(api.BattleStatusFinished, "", "ash", "gary"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.MyTurn(tc.b); got != tc.want {
				t.Errorf("MyTurn() = %v, want %v", got, tc.want)
			}
		})
	}
}

// SideFor must not depend on which slot the server happened to put this
// trainer in - the creator is index 0 and the joiner is index 1, so a
// client that assumed one would be wrong half the time.
func TestSideForWorksFromEitherSlot(t *testing.T) {
	c := &Client{Name: "ash"}

	mine, theirs, ok := c.SideFor(battle(api.BattleStatusActive, "ash", "ash", "gary"))
	if !ok || mine.Trainer != "ash" || theirs.Trainer != "gary" {
		t.Errorf("as creator: mine=%q theirs=%q ok=%v", mine.Trainer, theirs.Trainer, ok)
	}

	mine, theirs, ok = c.SideFor(battle(api.BattleStatusActive, "ash", "gary", "ash"))
	if !ok || mine.Trainer != "ash" || theirs.Trainer != "gary" {
		t.Errorf("as joiner: mine=%q theirs=%q ok=%v", mine.Trainer, theirs.Trainer, ok)
	}

	// A battle with one side is not yet playable; ok must say so rather
	// than the caller indexing past the end.
	if _, _, ok := c.SideFor(battle(api.BattleStatusWaiting, "", "ash")); ok {
		t.Error("SideFor reported ok for a battle with one side")
	}
}
