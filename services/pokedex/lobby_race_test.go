package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The cap holds against CONCURRENT creates, not just sequential ones.
//
// TestATrainerCannotFillTheLobby loops one create at a time, so it
// never overlaps the window between counting a trainer's open battles
// and inserting the next one. That window was the whole bug: two
// requests both read zero and both insert, because nothing held
// between the count and the write and no constraint backed it.
//
// Reproduced against production before the fix: 12 parallel creates
// from one token against a cap of 1 gave 8 successes, and that
// trainer then held the entire lobby - the exact abuse the cap exists
// to stop, committed in parallel instead of in sequence.
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
			<-start // released together, so they genuinely overlap
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

	// And the lobby agrees, which is the thing a player actually sees.
	open, err := store.waiting(ctx)
	if err != nil {
		t.Fatalf("listing the lobby: %v", err)
	}
	if len(open) != maxOpenBattlesPerTrainer {
		t.Errorf("lobby holds %d battles for one trainer, want %d", len(open), maxOpenBattlesPerTrainer)
	}
}

// Winning the race must not leave the trainer permanently blocked:
// the slot frees when someone joins.
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
