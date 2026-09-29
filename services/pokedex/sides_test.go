package main

import (
	"context"
	"testing"
)

func TestSidesAreWrittenOnJoinNotOnEveryTurn(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	b, tokA, _ := activeBattleOn(t, s)
	ctx := context.Background()

	countSides := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM battle_sides WHERE battle_id = $1", b.ID).Scan(&n); err != nil {
			t.Fatalf("counting sides: %v", err)
		}
		return n
	}

	if got := countSides(); got != 2 {
		t.Fatalf("after a join there are %d side rows, want 2", got)
	}

	// xmin is the transaction that last wrote the row, so an
	// unchanged xmin proves the row was not rewritten (rather than
	// merely rewritten to the same value).
	var before, after string
	if err := pool.QueryRow(ctx,
		"SELECT string_agg(xmin::text, ',' ORDER BY idx) FROM battle_sides WHERE battle_id = $1",
		b.ID).Scan(&before); err != nil {
		t.Fatalf("reading xmin: %v", err)
	}

	if err := s.battles.update(ctx, b.ID, func(bt *battle) error {
		bt.turnNumber++
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if err := pool.QueryRow(ctx,
		"SELECT string_agg(xmin::text, ',' ORDER BY idx) FROM battle_sides WHERE battle_id = $1",
		b.ID).Scan(&after); err != nil {
		t.Fatalf("reading xmin: %v", err)
	}
	if before != after {
		t.Errorf("a turn rewrote battle_sides (xmin %s -> %s); it should only be written on a join",
			before, after)
	}
	_ = tokA
}
