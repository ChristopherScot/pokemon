package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The embedded and DB loaders share buildPokedex; this pins that.
func TestBothSourcesBuildTheSameIndex(t *testing.T) {
	var doc dataset
	mustUnmarshalEmbedded(t, &doc)

	a, err := buildPokedex(doc.Pokemon, doc.Moves)
	if err != nil {
		t.Fatalf("building from embedded rows: %v", err)
	}

	// Same data reshaped as the DB path delivers it (int32, pointer
	// accuracy, moves via a join table).
	mons := make([]entry, 0, len(doc.Pokemon))
	for _, e := range doc.Pokemon {
		mons = append(mons, entry{
			ID:             int(int32(e.ID)),
			Name:           e.Name,
			Types:          e.Types,
			Height:         int(int32(e.Height)),
			Weight:         int(int32(e.Weight)),
			BaseHp:         int(int32(e.BaseHp)),
			BaseAttack:     int(int32(e.BaseAttack)),
			BaseDefense:    int(int32(e.BaseDefense)),
			BaseSpeed:      int(int32(e.BaseSpeed)),
			Description:    e.Description,
			Genus:          e.Genus,
			Habitat:        e.Habitat,
			Sprite:         e.Sprite,
			Artwork:        e.Artwork,
			EvolvesFrom:    e.EvolvesFrom,
			Legendary:      e.Legendary,
			Moves:          append([]string(nil), e.Moves...),
			LearnableMoves: append([]string(nil), e.LearnableMoves...),
		})
	}
	moves := make([]moveEntry, 0, len(doc.Moves))
	for _, m := range doc.Moves {
		me := moveEntry{
			Name: m.Name, Type: m.Type, Power: int(int32(m.Power)),
			Description: m.Description, Effect: m.Effect,
			PP: int(int32(m.PP)), DamageClass: m.DamageClass,
		}
		if m.Accuracy != nil {
			acc := int(int32(*m.Accuracy))
			me.Accuracy = &acc
		}
		moves = append(moves, me)
	}
	b, err := buildPokedex(mons, moves)
	if err != nil {
		t.Fatalf("building from db-shaped rows: %v", err)
	}

	if len(a.ordered) != len(b.ordered) {
		t.Fatalf("different sizes: %d vs %d", len(a.ordered), len(b.ordered))
	}
	if !reflect.DeepEqual(a.ordered, b.ordered) {
		t.Error("the two sources produced different Pokemon")
	}
	if !reflect.DeepEqual(a.moves, b.moves) {
		t.Error("the two sources produced different moves")
	}
	if !reflect.DeepEqual(a.stats, b.stats) {
		t.Error("the two sources produced different base stats")
	}
	if !reflect.DeepEqual(a.learnable, b.learnable) {
		t.Error("the two sources produced different learnable move lists")
	}
}

func mustUnmarshalEmbedded(t *testing.T, doc *dataset) {
	t.Helper()
	if err := json.Unmarshal(pokedexJSON, doc); err != nil {
		t.Fatalf("decoding the embedded pokedex: %v", err)
	}
}
