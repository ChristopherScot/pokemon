package main

import (
	"context"
	"testing"
	"time"
)

// A waiting battle's touched_at never advances (a join is its first
// update), so it must use waitingBattleTTL, not battleTTL.
func TestAWaitingBattleOutlivesTheActiveTTL(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	tok := registerFor(t, s, "patient")
	b := newTestBattle(t, dex, "patient", tok)
	if err := store.create(ctx, b); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Between the two TTLs.
	age := battleTTL + time.Hour
	if age >= waitingBattleTTL {
		t.Fatalf("test assumes battleTTL+1h < waitingBattleTTL; got %v vs %v", age, waitingBattleTTL)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE battles SET touched_at = now() - $1::interval, created_at = now() - $1::interval WHERE id = $2",
		age.String(), b.id); err != nil {
		t.Fatalf("ageing the battle: %v", err)
	}

	// waiting() sweeps before it lists.
	open, err := store.waiting(ctx)
	if err != nil {
		t.Fatalf("listing the lobby: %v", err)
	}
	found := false
	for _, w := range open {
		if w.BattleId == b.id {
			found = true
		}
	}
	if !found {
		t.Errorf("a battle waiting %v was swept away; a player polling the lobby "+
			"has it deleted under them and sees only 404", age)
	}
}

func TestAWaitingBattleIsStillSweptEventually(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	tok := registerFor(t, s, "gone")
	b := newTestBattle(t, dex, "gone", tok)
	if err := store.create(ctx, b); err != nil {
		t.Fatalf("creating: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE battles SET touched_at = now() - $1::interval WHERE id = $2",
		(waitingBattleTTL + time.Hour).String(), b.id); err != nil {
		t.Fatalf("ageing: %v", err)
	}

	if _, err := store.waiting(ctx); err != nil {
		t.Fatalf("listing: %v", err)
	}
	if _, err := store.get(ctx, b.id); err == nil {
		t.Error("a battle nobody joined for over a day should be swept")
	}
}

func TestAnActiveBattleStillExpiresOnTheShortTTL(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	host := registerFor(t, s, "host")
	b := newTestBattle(t, dex, "host", host)
	if err := store.create(ctx, b); err != nil {
		t.Fatalf("creating: %v", err)
	}
	joiner := registerFor(t, s, "joiner")
	if err := store.update(ctx, b.id, func(bt *battle) error {
		return bt.join("joiner", joiner, b.sides[0].team)
	}); err != nil {
		t.Fatalf("joining: %v", err)
	}

	if _, err := pool.Exec(ctx,
		"UPDATE battles SET touched_at = now() - $1::interval WHERE status = 'active'",
		(battleTTL + time.Hour).String()); err != nil {
		t.Fatalf("ageing: %v", err)
	}
	if _, err := store.waiting(ctx); err != nil {
		t.Fatalf("listing: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM battles WHERE status='active'").Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 0 {
		t.Errorf("%d active battles survived %v of silence, want 0", n, battleTTL+time.Hour)
	}
}
