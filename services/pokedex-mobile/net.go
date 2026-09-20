package main

// Everything that talks to the server.
//
// Gio's loop must never block: a frame that waits on a request freezes
// the UI mid-gesture. So every call runs on its own goroutine and
// delivers a result through a channel the loop drains between frames,
// then invalidates the window to draw it.
//
// The protocol itself is battleclient's, shared with the CLI and TUI.
// Nothing here reimplements a request - it only decides when to make
// one and what to do with the answer.

import (
	"context"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// defaultAPI matches the TUI's, so a phone and a terminal talk to the
// same server without configuration.
const defaultAPI = "https://pokemon.home.chrisscotmartin.com/api"

// requestTimeout bounds every call. A phone changes networks mid-tap -
// wifi to cellular, or a tunnel dropping - and without this the UI
// shows a spinner until the OS gives up, which can be minutes.
const requestTimeout = 15 * time.Second

// result is what a background call hands back to the UI loop.
type result struct {
	kind   resultKind
	dex    []api.Pokemon
	battle *api.Battle
	lobby  *api.WaitingList
	ident  battleclient.Identity
	err    error
}

type resultKind int

const (
	resDex resultKind = iota
	resBattle
	resLobby
	resRegistered
)

// go1 runs fn off the UI goroutine and posts its result.
//
// Named for what it guards: every network call in this app goes
// through here, so there is one place that owns the timeout, the
// panic-free delivery, and the window invalidation.
func (a *ui) go1(fn func(context.Context) result) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		r := fn(ctx)
		select {
		case a.results <- r:
		case <-time.After(requestTimeout):
			// The UI is gone; dropping the result is correct.
		}
		a.invalidate()
	}()
}

func (a *ui) loadDex() {
	a.busy = true
	a.go1(func(ctx context.Context) result {
		res, err := a.api.ListPokemon(ctx, api.ListPokemonParams{
			Limit: api.NewOptInt(200),
		})
		if err != nil {
			return result{kind: resDex, err: err}
		}
		return result{kind: resDex, dex: res.Pokemon}
	})
}

func (a *ui) register(name string) {
	a.busy = true
	a.go1(func(ctx context.Context) result {
		id, err := battleclient.Register(ctx, defaultAPI, name)
		return result{kind: resRegistered, ident: id, err: err}
	})
}

func (a *ui) loadLobby() {
	a.go1(func(ctx context.Context) result {
		if a.bc == nil {
			return result{kind: resLobby, err: errString("not registered")}
		}
		l, err := a.bc.Lobby(ctx)
		return result{kind: resLobby, lobby: l, err: err}
	})
}

func (a *ui) createBattle(team []string) {
	a.busy = true
	a.go1(func(ctx context.Context) result {
		b, err := a.bc.Create(ctx, team)
		return result{kind: resBattle, battle: b, err: err}
	})
}

func (a *ui) joinBattle(id string, team []string) {
	a.busy = true
	a.go1(func(ctx context.Context) result {
		b, err := a.bc.Join(ctx, id, team)
		return result{kind: resBattle, battle: b, err: err}
	})
}

func (a *ui) attack(id string, attacker, move, target int) {
	a.busy = true
	a.go1(func(ctx context.Context) result {
		b, err := a.bc.Attack(ctx, id, attacker, move, target)
		return result{kind: resBattle, battle: b, err: err}
	})
}

// watch long-polls for the opponent's move.
//
// Separate from the other calls because it is EXPECTED to take a long
// time - it returns when something happens, not immediately - so it
// gets its own generous deadline and does not set a.busy, or the UI
// would show a spinner for the whole of the opponent's turn.
func (a *ui) watch(id string, seen int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		b, err := a.bc.Watch(ctx, id, seen)
		// A watch timing out is normal, not an error worth showing:
		// it means nothing happened, and the caller polls again.
		if err != nil && ctx.Err() != nil {
			a.invalidate()
			return
		}
		select {
		case a.results <- result{kind: resBattle, battle: b, err: err}:
		case <-time.After(time.Second):
		}
		a.invalidate()
	}()
}

type errString string

func (e errString) Error() string { return string(e) }
