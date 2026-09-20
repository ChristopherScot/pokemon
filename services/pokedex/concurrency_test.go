package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestConcurrentHandlersDoNotRaceOnRNG(t *testing.T) {
	s := testService(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
