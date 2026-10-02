package main

// Every server call runs on its own goroutine and delivers via a channel; Gio's frame loop must never block.

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// apiBase is the server to talk to; default is chosen at compile time per platform, POKEDEX_URL overrides either.
func apiBase() string {
	if v := strings.TrimSpace(os.Getenv("POKEDEX_URL")); v != "" {
		return v
	}
	return defaultAPI
}

// requestTimeout bounds every call so a network change mid-tap does not hang the UI on the OS's own retry timeout.
const requestTimeout = 15 * time.Second

// watchTimeout bounds a long poll that deliberately waits for the opponent.
const watchTimeout = 2 * time.Minute

// dexPageSize is the spec's maximum for ListPokemon; asking for more is a 400.
const dexPageSize = 100

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
	// resWatchIdle: a long poll expired empty; distinct from an error so apply can re-arm silently.
	resWatchIdle
)

// go1 runs fn off the UI goroutine and posts its result; every network call goes through here.
// Callers MUST capture what they need before the call - a closure reaching back into ui races useIdentity's reassignment of a.bc.
func (u *ui) go1(timeout time.Duration, fn func(context.Context) result) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		r := fn(ctx)
		// Block on done rather than dropping: Android stops draining while backgrounded, and dropping leaves busy stuck true.
		select {
		case u.results <- r:
		case <-u.done:
			return
		}
		u.invalidate()
	}()
}

func (u *ui) loadDex() {
	client := u.api
	if client == nil {
		u.status = "no connection to the pokedex"
		return
	}
	u.busy = true
	u.go1(requestTimeout, func(ctx context.Context) result {
		res, err := client.ListPokemon(ctx, api.ListPokemonParams{
			Limit: api.NewOptInt(dexPageSize),
		})
		if err != nil {
			return result{kind: resDex, err: err}
		}
		return result{kind: resDex, dex: res.Pokemon}
	})
}

func (u *ui) register(name string) {
	u.busy = true
	u.go1(requestTimeout, func(ctx context.Context) result {
		id, err := battleclient.Register(ctx, apiBase(), name)
		return result{kind: resRegistered, ident: id, err: err}
	})
}

func (u *ui) loadLobby() {
	bc := u.bc
	if bc == nil {
		u.status = "register a trainer first"
		return
	}
	u.go1(requestTimeout, func(ctx context.Context) result {
		l, err := bc.Lobby(ctx)
		return result{kind: resLobby, lobby: l, err: err}
	})
}

func (u *ui) createBattle(team []string) {
	bc := u.bc
	if bc == nil {
		return
	}
	u.busy = true
	u.go1(requestTimeout, func(ctx context.Context) result {
		b, err := bc.Create(ctx, team)
		return result{kind: resBattle, battle: b, err: err}
	})
}

func (u *ui) joinBattle(id string, team []string) {
	bc := u.bc
	if bc == nil {
		return
	}
	u.busy = true
	u.go1(requestTimeout, func(ctx context.Context) result {
		b, err := bc.Join(ctx, id, team)
		return result{kind: resBattle, battle: b, err: err}
	})
}

func (u *ui) attack(id string, attacker, move, target int) {
	bc := u.bc
	if bc == nil {
		return
	}
	u.busy = true
	u.go1(requestTimeout, func(ctx context.Context) result {
		b, err := bc.Attack(ctx, id, attacker, move, target)
		return result{kind: resBattle, battle: b, err: err}
	})
}

// watch long-polls for the opponent's move; longer deadline than the rest, and no busy flag since it is expected to wait.
func (u *ui) watch(id string, seen int) {
	bc := u.bc
	if bc == nil {
		return
	}
	u.go1(watchTimeout, func(ctx context.Context) result {
		b, err := bc.Watch(ctx, id, seen)
		// errors.Is, not ctx.Err() != nil, so a server error landing after the deadline is not swallowed as idle.
		if errors.Is(err, context.DeadlineExceeded) {
			return result{kind: resWatchIdle}
		}
		return result{kind: resBattle, battle: b, err: err}
	})
}
