package main

import (
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func testClient(t *testing.T) *battleclient.Client {
	t.Helper()
	c, err := battleclient.New(battleclient.Identity{
		Name: "ash", Token: "test-token", API: "https://example.invalid/api",
	})
	if err != nil {
		t.Fatalf("battleclient.New: %v", err)
	}
	return c
}

func testMon(name string, hp int) api.BattlePokemon {
	return api.BattlePokemon{
		Name: name, Hp: hp, MaxHp: 20, Fainted: hp <= 0,
		Types: []string{"electric"},
		Moves: []api.Move{
			{Name: "thunderbolt", Type: "electric", Power: 90, Pp: 15, DamageClass: "special"},
			{Name: "growl", Type: "normal", Power: 0, Pp: 40, DamageClass: "status"},
		},
	}
}

func testBattle(me, them string) *api.Battle {
	return &api.Battle{
		ID: "b1", Status: "active", Version: 1,
		Turn: api.NewOptString(me),
		Sides: []api.Side{
			{Trainer: me, Team: []api.BattlePokemon{testMon("pikachu", 20), testMon("geodude", 8)}},
			{Trainer: them, Team: []api.BattlePokemon{testMon("staryu", 14), testMon("psyduck", 0)}},
		},
		Log: []api.BattleEvent{
			{TurnNumber: 1, Text: "pikachu used thunderbolt!"},
			{TurnNumber: 1, Text: "it's super effective!"},
			{TurnNumber: 2, Text: "psyduck fainted!"},
		},
	}
}
