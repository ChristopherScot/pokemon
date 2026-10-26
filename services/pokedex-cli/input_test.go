package main

import (
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// clientAs is a client that believes it is the named trainer, which is
// all printBattle needs to pick a side.
func clientAs(t *testing.T, name string) *battleclient.Client {
	t.Helper()
	c, err := battleclient.New(battleclient.Identity{
		Name: name, Token: "tok-" + name, API: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("building a client: %v", err)
	}
	return c
}

// A position is a whole number or it is a mistake.
//
// index1 used fmt.Sscanf, which stops at the first byte that does not
// match and reports success for the prefix it consumed. So "2x" and
// "1.9" and "2 3" all parsed, the remainder was discarded, and
// `attack abc 1 2.9 3` attacked with move 2 without saying anything.
// A typo in a battle command silently did something other than what
// was typed.
func TestAPositionMustBeAWholeNumber(t *testing.T) {
	for _, in := range []string{"2x", "3abc", "1.9", "2 3", "1,2", "1st"} {
		if got, err := index1(in, "move"); err == nil {
			t.Errorf("index1(%q) = %d with no error; trailing input must be "+
				"rejected, not silently dropped", in, got+1)
		}
	}
}

func TestAValidPositionStillParses(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int // zero-based
	}{
		{"1", 0}, {"2", 1}, {"  3  ", 2}, {"10", 9},
	} {
		got, err := index1(tc.in, "move")
		if err != nil {
			t.Errorf("index1(%q) errored: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("index1(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAPositionBelowOneIsRejected(t *testing.T) {
	for _, in := range []string{"0", "-1"} {
		if _, err := index1(in, "move"); err == nil {
			t.Errorf("index1(%q) was accepted; positions start at 1", in)
		}
	}
}

// Printing a battle must not panic, whatever the server sent.
//
// The usage hint read mine.Team[0].Moves on the same line that
// defensively read len(mine.Team) - so a side with an empty team was
// an index-out-of-range in the player's terminal rather than an
// error. A malformed response is an ordinary outcome for a client,
// not an invariant violation worth a stack trace.
func TestPrintingABattleSurvivesAnEmptyTeam(t *testing.T) {
	b := &api.Battle{
		ID:     "abc123",
		Status: api.BattleStatusActive,
		Turn:   api.NewOptString("ash"),
		Sides: []api.Side{
			{Trainer: "ash", Team: nil},
			{Trainer: "gary", Team: []api.BattlePokemon{{Name: "onix", Hp: 10, MaxHp: 10}}},
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("printing a battle with an empty team panicked: %v", r)
		}
	}()
	printBattle(clientAs(t, "ash"), b)
}

// And the hint describes a pokemon that can actually act, rather than
// whatever is in slot 0.
func TestTheUsageHintDescribesAUsablePokemon(t *testing.T) {
	fainted := api.BattlePokemon{Name: "pikachu", Hp: 0, MaxHp: 95, Fainted: true}
	fainted.SetCanAct(api.NewOptBool(false))
	ready := api.BattlePokemon{
		Name: "charmander", Hp: 99, MaxHp: 99,
		Moves: []api.Move{{Name: "ember"}, {Name: "scratch"}, {Name: "growl"}},
	}
	ready.SetCanAct(api.NewOptBool(true))

	b := &api.Battle{
		ID: "abc123", Status: api.BattleStatusActive, Turn: api.NewOptString("ash"),
		Sides: []api.Side{
			{Trainer: "ash", Team: []api.BattlePokemon{fainted, ready}},
			{Trainer: "gary", Team: []api.BattlePokemon{{Name: "onix"}}},
		},
	}
	out := captureStdout(t, func() { printBattle(clientAs(t, "ash"), b) })
	if !strings.Contains(out, "<move 1-3>") {
		t.Errorf("usage hint did not describe the usable pokemon's 3 moves; got:\n%s", out)
	}
}
