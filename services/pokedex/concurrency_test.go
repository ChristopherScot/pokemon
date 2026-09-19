package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Concurrency regression tests. Run with -race.
//
// Two real races lived here for months because every other test drives
// the engine from one goroutine, and the detector only finds what you
// exercise. These are the tests that would have caught them.

// One *rand.Rand was shared by every handler, and *rand.Rand is not
// safe for concurrent use - the detector reports it inside
// rngSource.Uint64 on any two simultaneous requests that roll damage. *rand.Rand is
// not safe for concurrent use, so two simultaneous requests race on its
// internal state. pgstore.go already documents this exact hazard for
// its retry jitter; the same reasoning was never applied here.
func TestConcurrentHandlersDoNotRaceOnRNG(t *testing.T) {
	s := testService(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Register gets a real token, so CreateBattle reaches
			// fillTeam(s.rng) and randomID(s.rng).
			r, err := s.RegisterTrainer(context.Background(), &api.RegisterTrainer{Name: name()})
			if err != nil {
				return
			}
			tr, ok := r.(*api.Trainer)
			if !ok {
				return
			}
			_, _ = s.CreateBattle(context.Background(), &api.CreateBattle{},
				api.CreateBattleParams{XTrainerToken: tr.Token})
		}()
	}
	wg.Wait()
}

var n atomic.Int64

func name() string { return fmt.Sprintf("t%d", n.Add(1)) }

// memStore.get used to return the *battle and release the lock, and
// the handler then called toAPI() outside it - reading sides, log and
// every combatant while another request mutated exactly those fields.
// The mutex was protecting the map, not the battle the map points at.
func TestReadingABattleDoesNotRaceWithATurn(t *testing.T) {
	s := testService(t)
	b, tokA, _ := activeBattle(t, s)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = s.GetBattle(context.Background(), api.GetBattleParams{ID: b.id})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = s.TakeTurn(context.Background(), &api.TakeTurn{},
				api.TakeTurnParams{ID: b.id, XTrainerToken: tokA})
		}
	}()
	wg.Wait()
}
