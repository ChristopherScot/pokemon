package main

// Concurrency tests that assert something.
//
// The previous versions of this file spawned goroutines, discarded
// every error and every result, and contained no t.Error at all -
// they passed if every operation failed. Worse, they ran against
// memStore, so the file named for the riskiest behaviour in the
// service exercised a sync.Mutex in one process and never touched
// the SERIALIZABLE transaction, the isolation level or the retry
// loop that production depends on.
//
// These run against Postgres and pin the invariants.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

var n atomic.Int64

func name() string { return fmt.Sprintf("t%d", n.Add(1)) }

// Exactly one of N simultaneous turns is accepted.
//
// This is the property the whole SERIALIZABLE-plus-retry design
// exists for. Without it two attacks interleave and a battle either
// loses a turn or takes two from the same trainer.
func TestOnlyOneSimultaneousTurnIsAccepted(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	b, tokA, _ := activeBattleOn(t, s)

	const attempts = 8
	var accepted, conflicts atomic.Int64
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.TakeTurn(context.Background(), &api.TakeTurn{},
				api.TakeTurnParams{ID: b.ID, XTrainerToken: tokA})
			if err != nil {
				t.Errorf("TakeTurn returned a hard error: %v", err)
				return
			}
			switch res.(type) {
			case *api.Battle:
				accepted.Add(1)
			case *api.TakeTurnConflict:
				conflicts.Add(1)
			default:
				t.Errorf("unexpected response %T", res)
			}
		}()
	}
	wg.Wait()

	if got := accepted.Load(); got != 1 {
		t.Errorf("%d of %d simultaneous turns were accepted, want exactly 1",
			got, attempts)
	}
	if got := conflicts.Load(); got != attempts-1 {
		t.Errorf("%d conflicts, want %d - the rest should have been refused",
			got, attempts-1)
	}
}

// Concurrent writers must all land. A serialization failure that the
// retry loop gives up on is a silently lost turn.
func TestNoWriteIsLostUnderContention(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	b, _, _ := activeBattleOn(t, s)

	const writers = 10
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each writer appends one distinguishable line.
			err := store.update(context.Background(), b.ID, func(bt *battle) error {
				bt.log = append(bt.log, api.BattleEvent{
					TurnNumber: i,
					Text:       fmt.Sprintf("writer %d", i),
				})
				return nil
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a write failed: %v", err)
	}

	got, err := store.get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	// The battle starts with its own log lines, so count only ours.
	mine := 0
	for _, ev := range got.Log {
		if len(ev.Text) > 6 && ev.Text[:6] == "writer" {
			mine++
		}
	}
	if mine != writers {
		t.Errorf("%d of %d writes survived; the rest were lost", mine, writers)
	}
}

// Reading a battle while it is being written must not observe a
// half-applied turn, and must not race.
func TestReadsSeeWholeTurnsOnly(t *testing.T) {
	_, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	b, tokA, tokB := activeBattleOn(t, s)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 25 {
			got, err := store.get(context.Background(), b.ID)
			if err != nil {
				t.Errorf("read during a turn failed: %v", err)
				return
			}
			// A half-applied turn would show a side with the wrong
			// number of Pokemon, or a battle with one side.
			if len(got.Sides) != 2 {
				t.Errorf("observed %d sides mid-turn, want 2", len(got.Sides))
				return
			}
			for _, side := range got.Sides {
				if len(side.Team) != teamSize {
					t.Errorf("observed a team of %d mid-turn, want %d",
						len(side.Team), teamSize)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 25 {
			tok := tokA
			if i%2 == 1 {
				tok = tokB
			}
			// Conflicts are expected and fine; a hard error is not.
			if _, err := s.TakeTurn(context.Background(), &api.TakeTurn{},
				api.TakeTurnParams{ID: b.ID, XTrainerToken: tok}); err != nil {
				t.Errorf("turn failed: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

// activeBattleOn builds a started battle on whatever store the
// service holds, and returns both trainers' tokens.
func activeBattleOn(t *testing.T, s service) (*api.Battle, string, string) {
	t.Helper()
	ctx := context.Background()

	tokA, err := s.battles.registerTrainer(ctx, name())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	tokB, err := s.battles.registerTrainer(ctx, name())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	created, err := s.CreateBattle(ctx, &api.CreateBattle{
		Team: []string{"bulbasaur", "charmander", "squirtle"},
	}, api.CreateBattleParams{XTrainerToken: tokA})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	bt, ok := created.(*api.Battle)
	if !ok {
		t.Fatalf("create returned %#v", created)
	}

	joined, err := s.JoinBattle(ctx, &api.JoinBattle{
		Team: []string{"pikachu", "geodude", "caterpie"},
	}, api.JoinBattleParams{ID: bt.ID, XTrainerToken: tokB})
	if err != nil {
		t.Fatalf("joining: %v", err)
	}
	if _, ok := joined.(*api.Battle); !ok {
		t.Fatalf("join returned %#v", joined)
	}
	return bt, tokA, tokB
}
