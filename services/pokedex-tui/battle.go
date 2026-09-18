package main

// The battle screen.
//
// Animation here is entirely presentational: the server sends the new HP
// and this interpolates towards it over a few frames. Sending
// intermediate values would make the API's state ambiguous - a client
// asking "what is the HP" would get a number that depends on when it
// asked.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// frameRate drives HP drain and damage floats. 30fps is smooth enough
// for a bar and cheap enough that a terminal over ssh keeps up.
const frameRate = time.Second / 30

// drainPerFrame is how much HP a bar gives up per frame. Tuned so a
// heavy hit takes roughly half a second to play out: fast enough not to
// delay the next turn, slow enough to read as damage rather than a jump.
const drainPerFrame = 4

// frameMsg advances animations.
type frameMsg time.Time

// battleMsg carries new state from a poll.
type battleMsg struct {
	battle *api.Battle
	err    error
}

// lobbyMsg carries the list of open battles.
type lobbyMsg struct {
	list *api.WaitingList
	err  error
}

// shownHP is the HP a bar is currently drawing, which lags the real
// value while a drain animates. Keyed by side and slot, because a name
// is not unique - both trainers can bring the same Pokemon.
type slot struct {
	side, index int
}

// battleState is everything the battle screen needs. Split from model so
// the browse screen's fields are not tangled with it.
type battleState struct {
	client *battleclient.Client
	id     string
	battle *api.Battle
	err    error

	// shown lags battle for the drain animation.
	shown map[slot]int

	// floats are damage numbers rising off a Pokemon. They expire.
	floats []damageFloat

	// impacts are the shake-and-flash on a row that was just hit.
	impacts map[slot]*impact

	// banner pulses for a few frames when the turn changes, so a player
	// who looked away notices it is their move.
	bannerPulse int

	// Cursor state for choosing a move.
	pickAttacker int
	pickMove     int
	pickTarget   int
	focus        pickFocus

	// seen is the last version rendered, so a poll that returns nothing
	// new does not restart animations.
	seen int

	// logFrom is how much of the log has scrolled past, so a long battle
	// does not push the board off screen.
	logFrom int
}

type pickFocus int

const (
	focusAttacker pickFocus = iota
	focusMove
	focusTarget
)

type damageFloat struct {
	slot   slot
	amount int
	effect float64
	// frames remaining; the float rises and fades as this counts down.
	life int
}

// impact is a hit landing: the row shakes and flashes for a few frames.
// Separate from the float because it decays faster - the number should
// still be readable after the row has settled.
type impact struct {
	slot   slot
	effect float64
	life   int
}

const (
	floatLife  = 24
	impactLife = 7

	// A KO gets its own, longer flash, because it is the moment worth
	// noticing in a battle.
	faintLife = 14
)

// tick schedules the next animation frame.
func tick() tea.Cmd {
	return tea.Tick(frameRate, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// poll asks for state after a short delay, which is what keeps two
// clients in step without websockets.
func pollBattle(c *battleclient.Client, id string) tea.Cmd {
	return tea.Tick(battleclient.PollInterval, func(time.Time) tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		b, err := c.Get(ctx, id)
		return battleMsg{battle: b, err: err}
	})
}

func fetchLobby(c *battleclient.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		l, err := c.Lobby(ctx)
		return lobbyMsg{list: l, err: err}
	}
}

// applyBattle folds new server state in, starting animations for
// anything that changed.
func (bs *battleState) applyBattle(b *api.Battle) {
	if bs.shown == nil {
		bs.shown = map[slot]int{}
	}
	prev := bs.battle
	bs.battle = b

	for si, side := range b.Sides {
		for pi, p := range side.Team {
			k := slot{si, pi}
			if _, ok := bs.shown[k]; !ok {
				// First sight of this Pokemon: draw it at its real HP
				// rather than animating up from zero.
				bs.shown[k] = p.Hp
			}
		}
	}

	// Damage floats come from the log rather than from diffing HP: the
	// log already says who hit whom for how much, and a diff cannot tell
	// one big hit from two small ones in the same poll.
	if prev != nil && b.Version > prev.Version {
		for _, ev := range b.Log[min(len(prev.Log), len(b.Log)):] {
			dmg, ok := ev.Damage.Get()
			if !ok || dmg == 0 {
				continue
			}
			target, ok := ev.Target.Get()
			if !ok {
				continue
			}
			if k, found := bs.findSlot(target); found {
				eff := ev.Effectiveness.Or(1)
				bs.floats = append(bs.floats, damageFloat{
					slot:   k,
					amount: dmg,
					effect: eff,
					life:   floatLife,
				})
				if bs.impacts == nil {
					bs.impacts = map[slot]*impact{}
				}
				life := impactLife
				if ev.Fainted.Or(false) {
					life = faintLife
				}
				bs.impacts[k] = &impact{slot: k, effect: eff, life: life}
			}
		}
	}
	// A turn arriving is worth announcing: the banner pulses so someone
	// who looked away sees it change rather than having to read it.
	if prev != nil && bs.client.MyTurn(b) && !bs.client.MyTurn(prev) {
		bs.bannerPulse = 18
	}

	bs.seen = b.Version
	bs.clampCursors()
}

// findSlot locates a Pokemon by name on the side that is NOT the
// viewer's, falling back to either side. Damage lands on a target, and a
// target is on the opponent's side by definition.
func (bs *battleState) findSlot(name string) (slot, bool) {
	if bs.battle == nil {
		return slot{}, false
	}
	for si, side := range bs.battle.Sides {
		for pi, p := range side.Team {
			if p.Name == name {
				return slot{si, pi}, true
			}
		}
	}
	return slot{}, false
}

// advance moves every animation on by one frame, and reports whether
// anything is still moving - so the model can stop ticking when the
// screen is static.
func (bs *battleState) advance() bool {
	moving := false

	if bs.battle != nil {
		for si, side := range bs.battle.Sides {
			for pi, p := range side.Team {
				k := slot{si, pi}
				cur := bs.shown[k]
				if cur > p.Hp {
					cur -= drainPerFrame
					if cur < p.Hp {
						cur = p.Hp
					}
					bs.shown[k] = cur
					moving = true
				} else if cur < p.Hp {
					// Healing does not exist yet, but snapping up rather
					// than ignoring it keeps the bar honest if it does.
					bs.shown[k] = p.Hp
					moving = true
				}
			}
		}
	}

	for k, im := range bs.impacts {
		im.life--
		if im.life <= 0 {
			delete(bs.impacts, k)
		}
		moving = true
	}

	if bs.bannerPulse > 0 {
		bs.bannerPulse--
		moving = true
	}

	live := bs.floats[:0]
	for _, f := range bs.floats {
		f.life--
		if f.life > 0 {
			live = append(live, f)
			moving = true
		}
	}
	bs.floats = live

	return moving
}

// clampCursors keeps the selection inside the team after a faint, so the
// cursor never points at a Pokemon that cannot act.
func (bs *battleState) clampCursors() {
	b := bs.battle
	if b == nil || len(b.Sides) < 2 {
		return
	}
	mine, theirs, ok := bs.client.SideFor(b)
	if !ok {
		return
	}
	bs.pickAttacker = clampAlive(mine.Team, bs.pickAttacker)
	bs.pickTarget = clampAlive(theirs.Team, bs.pickTarget)
	if bs.pickAttacker < len(mine.Team) {
		if n := len(mine.Team[bs.pickAttacker].Moves); n > 0 && bs.pickMove >= n {
			bs.pickMove = n - 1
		}
	}
}

// clampAlive moves an index onto a Pokemon that is still standing.
func clampAlive(team []api.BattlePokemon, i int) int {
	if len(team) == 0 {
		return 0
	}
	if i >= 0 && i < len(team) && !team[i].Fainted {
		return i
	}
	for j, p := range team {
		if !p.Fainted {
			return j
		}
	}
	return 0
}
