package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestAPokedexWithAZeroBaseStatIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stats baseStats
	}{
		{"hp", baseStats{hp: 0, attack: 5, defense: 5, speed: 5}},
		{"attack", baseStats{hp: 5, attack: 0, defense: 5, speed: 5}},
		{"defense", baseStats{hp: 5, attack: 5, defense: 0, speed: 5}},
		{"speed", baseStats{hp: 5, attack: 5, defense: 5, speed: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pokedex{
				stats:   map[int]baseStats{1: tc.stats},
				ordered: []api.Pokemon{{ID: 1, Name: "bulbasaur"}},
			}
			err := p.finish()
			if err == nil {
				t.Fatalf("a zero %s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), "bulbasaur") {
				t.Errorf("error = %q, want it to name the entry at fault", err)
			}
		})
	}
}

func TestAPokedexEntryWithNoStatsIsRefused(t *testing.T) {
	p := &pokedex{
		stats:   map[int]baseStats{},
		ordered: []api.Pokemon{{ID: 1, Name: "bulbasaur"}},
	}
	if err := p.finish(); err == nil {
		t.Fatal("an entry with no base stats was accepted")
	}
}

func TestFinishPutsEntriesInDexOrder(t *testing.T) {
	good := baseStats{hp: 5, attack: 5, defense: 5, speed: 5}
	p := &pokedex{
		stats:   map[int]baseStats{1: good, 4: good, 7: good},
		ordered: []api.Pokemon{{ID: 7, Name: "squirtle"}, {ID: 1, Name: "bulbasaur"}, {ID: 4, Name: "charmander"}},
	}
	if err := p.finish(); err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, m := range p.ordered {
		ids = append(ids, m.ID)
	}
	if !slices.IsSorted(ids) {
		t.Errorf("ordered = %v, want dex order", ids)
	}
}
