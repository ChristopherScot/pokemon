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
	"errors"
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

// watchTimeout bounds a long poll. Longer than a request because it
// deliberately waits for the opponent.
const watchTimeout = 2 * time.Minute

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
	// resWatchIdle means a long poll expired with nothing to report.
	// Distinct from an error so apply can re-arm the watch without
	// showing the player anything.
	resWatchIdle
)

// go1 runs fn off the UI goroutine and posts its result.
//
// Named for what it guards: every network call goes through here, so
// there is one place that owns the timeout, the delivery and the
// window invalidation.
//
// THE RULE for anything passed here: capture what you need BEFORE the
// call. A closure that reaches back into the ui struct is reading
// fields the UI goroutine owns and may be writing - that was a real
// race on a.bc, which useIdentity reassigns while a lobby load is in
// flight.
func (u *ui) go1(timeout time.Duration, fn func(context.Context) result) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		r := fn(ctx)
		// done rather than a timer: a full channel means the UI is
		// BUSY, not gone - on Android it stops draining whenever the
		// activity is backgrounded. Dropping the result there leaves
		// busy stuck true and the app looking frozen, so this blocks
		// until the UI catches up and exits only when the window does.
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
			Limit: api.NewOptInt(200),
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
		id, err := battleclient.Register(ctx, defaultAPI, name)
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

// watch long-polls for the opponent's move.
//
// A longer deadline than the rest, and no busy flag: it is EXPECTED to
// take a while - it returns when something happens - so a spinner for
// the whole of the opponent's turn would be wrong.
func (u *ui) watch(id string, seen int) {
	bc := u.bc
	if bc == nil {
		return
	}
	u.go1(watchTimeout, func(ctx context.Context) result {
		b, err := bc.Watch(ctx, id, seen)
		// A watch hitting its own deadline is normal: nothing
		// happened and the caller polls again. Checked with
		// errors.Is rather than ctx.Err() != nil, which also
		// swallows a genuine server error that happened to land
		// after the deadline.
		if errors.Is(err, context.DeadlineExceeded) {
			return result{kind: resWatchIdle}
		}
		return result{kind: resBattle, battle: b, err: err}
	})
}
