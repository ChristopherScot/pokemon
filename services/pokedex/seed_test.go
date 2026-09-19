package main

// Seeding tests, against a real Postgres. Skipped without
// POKEDEX_TEST_DSN, like the migration tests.

import (
	"context"
	"testing"
)

// The whole point: what comes back out of the database is what went
// in, including the parts that are easy to lose - move ordering, the
// learnable set, and the optional fields.
func TestSeedRoundTrip(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := seed(ctx, pool); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fromDB, err := loadPokedexFromDB(ctx, pool)
	if err != nil {
		t.Fatalf("loading from db: %v", err)
	}
	fromFile, err := loadPokedex()
	if err != nil {
		t.Fatalf("loading from file: %v", err)
	}

	if len(fromDB.ordered) != len(fromFile.ordered) {
		t.Fatalf("database has %d pokemon, file has %d",
			len(fromDB.ordered), len(fromFile.ordered))
	}
	if len(fromDB.moves) != len(fromFile.moves) {
		t.Errorf("database has %d moves, file has %d",
			len(fromDB.moves), len(fromFile.moves))
	}

	for i, want := range fromFile.ordered {
		got := fromDB.ordered[i]
		if got.ID != want.ID || got.Name != want.Name {
			t.Fatalf("pokemon %d: got %d/%s, want %d/%s",
				i, got.ID, got.Name, want.ID, want.Name)
		}
		if got.Description != want.Description {
			t.Errorf("%s: description differs", want.Name)
		}
		if got.Genus != want.Genus {
			t.Errorf("%s: genus %q, want %q", want.Name, got.Genus, want.Genus)
		}
		if got.Habitat != want.Habitat {
			t.Errorf("%s: habitat %v, want %v", want.Name, got.Habitat, want.Habitat)
		}
		if got.Legendary != want.Legendary {
			t.Errorf("%s: legendary %v, want %v", want.Name, got.Legendary, want.Legendary)
		}

		// Move ORDER, not just membership. The battle picks by index,
		// so a set that matches in the wrong order is a different
		// game.
		if len(got.Moves) != len(want.Moves) {
			t.Errorf("%s: %d moves, want %d", want.Name, len(got.Moves), len(want.Moves))
			continue
		}
		for j := range want.Moves {
			if got.Moves[j].Name != want.Moves[j].Name {
				t.Errorf("%s move %d: %q, want %q",
					want.Name, j, got.Moves[j].Name, want.Moves[j].Name)
			}
		}
	}

	// The learnable set, which is the larger list and a different code
	// path from the battle moves.
	for name, want := range fromFile.learnable {
		got := fromDB.learnable[name]
		if len(got) != len(want) {
			t.Errorf("%s: %d learnable moves, want %d", name, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s learnable %d: %q, want %q", name, i, got[i], want[i])
				break
			}
		}
	}
}

// Seeding runs on every start, so the second one must be a no-op
// rather than a duplicate-key error.
func TestSeedIsIdempotent(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := seed(ctx, pool); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := seed(ctx, pool); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	var pokemon, moves, links int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pokemon").Scan(&pokemon); err != nil {
		t.Fatalf("counting pokemon: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM moves").Scan(&moves); err != nil {
		t.Fatalf("counting moves: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pokemon_moves").Scan(&links); err != nil {
		t.Fatalf("counting links: %v", err)
	}

	fromFile, err := loadPokedex()
	if err != nil {
		t.Fatalf("loading from file: %v", err)
	}
	if pokemon != len(fromFile.ordered) {
		t.Errorf("%d pokemon after two seeds, want %d", pokemon, len(fromFile.ordered))
	}
	if moves != len(fromFile.moves) {
		t.Errorf("%d moves after two seeds, want %d", moves, len(fromFile.moves))
	}
	if links == 0 {
		t.Error("no pokemon_moves rows after seeding")
	}
}
