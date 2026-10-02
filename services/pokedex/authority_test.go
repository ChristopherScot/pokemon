package main

import (
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// Clients must obey CanAct/UsableMoves, even when fainted and
// DisabledMove alone would say otherwise.
func TestClientObeysTheServerNotItsOwnRules(t *testing.T) {
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

	allowed := api.BattlePokemon{
		Name: "geodude", Hp: 20, MaxHp: 20,
		Moves:        []api.Move{{Name: "tackle", Power: 40}},
		DisabledMove: api.NewOptInt(0),
		UsableMoves:  []bool{true},
	}
	if !battleclient.MoveUsable(allowed, 0) {
		t.Error("client refused a move the server allowed; it is still using its own rule")
	}
}
