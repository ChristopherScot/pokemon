package main

// Guards the invariant that a background closure captures a.bc before spawning; useIdentity may reassign it mid-flight.

import (
	"sync"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func TestBackgroundCallsTouchNothingTheUIOwns(t *testing.T) {
	a := newUI(nil)
	a.bc, _ = battleclient.New(battleclient.Identity{
		Name: "ash", Token: "t", API: "https://example.invalid/api",
	})

	// Simulate registration reassigning the client mid-flight, interleaved as tightly as possible.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			a.loadLobby()
			a.useIdentity(battleclient.Identity{
				Name: "ash", Token: "t2", API: "https://example.invalid/api",
			})
		}
	}()
	wg.Wait()

	// Closes the goroutines' select and lets them finish — this is the test signal, not cleanup.
	close(a.done)
}
