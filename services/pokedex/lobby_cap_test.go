package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The lobby is ORDER BY created_at DESC LIMIT 100, so an uncapped
// opener can push everyone else out.
func TestATrainerCannotFillTheLobby(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	tok := registerFor(t, s, "hoarder")

	const attempts = 20
	var created, refused atomic.Int64
	for i := 0; i < attempts; i++ {
		res, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: tok})
		if err != nil {
			t.Fatalf("creating battle %d: %v", i, err)
		}
		switch res.(type) {
		case *api.Battle:
			created.Add(1)
		case *api.CreateBattleConflict:
			refused.Add(1)
		default:
			t.Fatalf("battle %d got %T, want a Battle or a Conflict", i, res)
		}
	}

	if got := created.Load(); got != maxOpenBattlesPerTrainer {
		t.Errorf("one trainer opened %d battles, want at most %d - the lobby is the "+
			"newest 100, so the rest are invisible to every other player",
			got, maxOpenBattlesPerTrainer)
	}
	if got := refused.Load(); got != attempts-maxOpenBattlesPerTrainer {
		t.Errorf("%d attempts were refused, want %d", got, attempts-maxOpenBattlesPerTrainer)
	}
}

// A global cap would pass the previous test while breaking the game.
func TestTheCapIsPerTrainerNotGlobal(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	for _, name := range []string{"ash", "misty", "brock"} {
		res, err := s.CreateBattle(ctx, &api.CreateBattle{},
			api.CreateBattleParams{XTrainerToken: registerFor(t, s, name)})
		if err != nil {
			t.Fatalf("%s creating a battle: %v", name, err)
		}
		if _, ok := res.(*api.Battle); !ok {
			t.Errorf("%s got %T opening their FIRST battle, want a Battle", name, res)
		}
	}

	open, err := store.waiting(ctx)
	if err != nil {
		t.Fatalf("listing the lobby: %v", err)
	}
	if len(open) != 3 {
		t.Errorf("lobby has %d battles, want 3 - one per trainer", len(open))
	}
}

// The cap counts WAITING battles, not battles ever opened.
func TestTheSlotIsFreedWhenTheBattleIsJoined(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()

	tok := registerFor(t, s, "opener")
	first, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: tok})
	if err != nil {
		t.Fatalf("first battle: %v", err)
	}
	b, ok := first.(*api.Battle)
	if !ok {
		t.Fatalf("first battle got %T", first)
	}

	joiner := registerFor(t, s, "joiner")
	if _, err := s.JoinBattle(ctx, &api.JoinBattle{},
		api.JoinBattleParams{ID: b.ID, XTrainerToken: joiner}); err != nil {
		t.Fatalf("joining: %v", err)
	}

	res, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: tok})
	if err != nil {
		t.Fatalf("second battle: %v", err)
	}
	if _, ok := res.(*api.Battle); !ok {
		t.Errorf("got %T opening a battle after the first was joined, want a Battle - "+
			"the cap counts WAITING battles, not battles ever opened", res)
	}
}

func registerFor(t *testing.T, s service, name string) string {
	t.Helper()
	tok, err := s.battles.registerTrainer(context.Background(), name)
	if err != nil {
		t.Fatalf("registering %s: %v", name, err)
	}
	return tok
}
