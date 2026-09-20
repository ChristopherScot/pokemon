package main

import (
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// The point of moving the rules server-side: a client must follow the
// server's verdict even when its own inputs would say otherwise.
//
// Before, CheckTurn re-derived legality from fainted and disabledMove,
// so a rule the server changed and the client did not know about was
// silently ignored. This asserts the client has stopped deciding.
func TestClientObeysTheServerNotItsOwnRules(t *testing.T) {
	// A Pokemon the server says cannot act, while every input a
	// client used to reason from says it can: not fainted, no
	// disabled move.
	mon := api.BattlePokemon{
		Name: "pikachu", Hp: 20, MaxHp: 20, Fainted: false,
		Moves:       []api.Move{{Name: "thunderbolt", Power: 90}},
		CanAct:      api.NewOptBool(false),
		UsableMoves: []bool{false},
	}
	if battleclient.CanAct(mon) {
		t.Error("client said a Pokemon can act after the server said it cannot")
	}
	if battleclient.MoveUsable(mon, 0) {
		t.Error("client offered a move the server marked unusable")
	}

	// And the reverse: the server permits something the old
	// disabledMove rule would have refused.
	allowed := api.BattlePokemon{
		Name: "geodude", Hp: 20, MaxHp: 20,
		Moves:        []api.Move{{Name: "tackle", Power: 40}},
		DisabledMove: api.NewOptInt(0), // the old rule says no
		UsableMoves:  []bool{true},     // the server says yes
	}
	if !battleclient.MoveUsable(allowed, 0) {
		t.Error("client refused a move the server allowed; it is still using its own rule")
	}
}
