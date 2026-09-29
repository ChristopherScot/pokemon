package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Sequential cap tests never overlap the count-then-insert window
// that races can drive through; this one does.
func TestTheLobbyCapHoldsUnderConcurrentCreates(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()
	tok := registerFor(t, s, "racer")

	const attempts = 16
	var created, refused, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := s.CreateBattle(ctx, &api.CreateBattle{},
				api.CreateBattleParams{XTrainerToken: tok})
			if err != nil {
				other.Add(1)
				return
			}
			switch res.(type) {
			case *api.Battle:
				created.Add(1)
			case *api.CreateBattleConflict:
				refused.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := created.Load(); got != maxOpenBattlesPerTrainer {
		t.Errorf("%d of %d simultaneous creates succeeded, want exactly %d - "+
			"a cap that only holds when requests arrive one at a time is no cap "+
			"on an internet-facing service", got, attempts, maxOpenBattlesPerTrainer)
	}
	if n := other.Load(); n != 0 {
		t.Errorf("%d creates failed with something other than a 409; the race "+
			"should be refused cleanly, not surfaced as a 500", n)
	}

	open, err := store.waiting(ctx)
	if err != nil {
		t.Fatalf("listing the lobby: %v", err)
	}
	if len(open) != maxOpenBattlesPerTrainer {
		t.Errorf("lobby holds %d battles for one trainer, want %d", len(open), maxOpenBattlesPerTrainer)
	}
}

func TestTheSlotStillFreesAfterAContendedCreate(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()
	tok := registerFor(t, s, "opener")

	res, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: tok})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	b, ok := res.(*api.Battle)
	if !ok {
		t.Fatalf("first create got %T", res)
	}

	joiner := registerFor(t, s, "joiner")
	if _, err := s.JoinBattle(ctx, &api.JoinBattle{},
		api.JoinBattleParams{ID: b.ID, XTrainerToken: joiner}); err != nil {
		t.Fatalf("joining: %v", err)
	}

	again, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: tok})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if _, ok := again.(*api.Battle); !ok {
		t.Errorf("got %T opening a battle after the first was joined, want a Battle - "+
			"the constraint must be released when the battle stops waiting, or the "+
			"cap becomes one battle per trainer EVER", again)
	}
}
