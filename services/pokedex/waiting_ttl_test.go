package main

import (
	"context"
	"testing"
	"time"
)

// A player waiting in the lobby is not abandoned.
//
// touched_at only advances on UpdateBattle, and a waiting battle has
// no updates - a join is its first. Reads do not touch it, so polling
// does not either. Under one cutoff that meant a battle opened and
// watched attentively was deleted at exactly battleTTL, and the client
// saw a bare 404 it could not tell from a wrong id.
func TestAWaitingBattleOutlivesTheActiveTTL(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	tok := registerFor(t, s, "patient")
	b := newTestBattle(t, dex, "patient", tok)
	if err := store.create(ctx, b); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Older than the active TTL, younger than the waiting one: the
	// exact window the single cutoff got wrong.
	age := battleTTL + time.Hour
	if age >= waitingBattleTTL {
		t.Fatalf("test assumes battleTTL+1h < waitingBattleTTL; got %v vs %v", age, waitingBattleTTL)
	}
	if _, err := pool.Exec(ctx,
		"UPDATE battles SET touched_at = now() - $1::interval, created_at = now() - $1::interval WHERE id = $2",
		age.String(), b.id); err != nil {
		t.Fatalf("ageing the battle: %v", err)
	}

	// waiting() sweeps before it lists, so this both triggers and checks.
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

// The longer TTL must not mean waiting battles never expire.
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

// An ACTIVE battle keeps the short TTL: both players having walked away
// mid-game is what it is there to clean up.
func TestAnActiveBattleStillExpiresOnTheShortTTL(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	// An active battle: one opened, one joined.
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
