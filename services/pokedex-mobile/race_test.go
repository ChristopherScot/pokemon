package main

// The race the review found, in the shape the app can actually reach:
// loadLobby and useIdentity both run on the UI goroutine, but the
// goroutine loadLobby SPAWNS must not touch anything the UI owns.
//
// Before the fix the spawned closure dereferenced a.bc directly, so a
// registration landing mid-flight was a genuine race. It now captures
// the client first, and this test fails if that regresses.

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

	// The UI goroutine: spawn work, then reassign the client the way
	// registration does, interleaved as tightly as possible.
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

	// Let the spawned requests run against a dead host and report.
	close(a.done)
}
