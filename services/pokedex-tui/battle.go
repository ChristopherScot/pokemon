package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

const frameRate = time.Second / 30

const drainPerFrame = 4

// frameMsg advances animations.
type frameMsg time.Time

// battleMsg carries new state from a poll.
type battleMsg struct {
	battle *api.Battle
	err    error

	misses int
}

// lobbyMsg carries the list of open battles.
type lobbyMsg struct {
	polled bool
	list   *api.WaitingList
	err    error
}

type slot struct {
	side, index int
}

const maxPollMisses = 5

type battleState struct {
	client *battleclient.Client
	id     string
	battle *api.Battle
	err    error

	misses int

	// shown lags battle for the drain animation.
	shown map[slot]int

	// floats are damage numbers rising off a Pokemon. They expire.
	floats []damageFloat

	// impacts are the shake-and-flash on a row that was just hit.
	impacts map[slot]*impact

	bannerPulse int

	// Cursor state for choosing a move.
	pickAttacker int
	pickMove     int
	pickTarget   int
	focus        pickFocus

	seen int

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

type impact struct {
	slot   slot
	effect float64
	life   int
}

const (
	floatLife  = 24
	impactLife = 7

	faintLife = 14
)

// tick schedules the next animation frame.
func tick() tea.Cmd {
	return tea.Tick(frameRate, func(t time.Time) tea.Msg { return frameMsg(t) })
}

func pollBattle(c *battleclient.Client, id string) tea.Cmd {
	return tea.Tick(battleclient.PollInterval, func(time.Time) tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		b, err := c.Get(ctx, id)
		return battleMsg{battle: b, err: err}
	})
}

func pollLobby(c *battleclient.Client) tea.Cmd {
	return tea.Tick(battleclient.LobbyPollInterval, func(time.Time) tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		l, err := c.Lobby(ctx)
		return lobbyMsg{list: l, err: err, polled: true}
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
				bs.shown[k] = p.Hp
			}
		}
	}

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
	if prev != nil && bs.client.MyTurn(b) && !bs.client.MyTurn(prev) {
		bs.bannerPulse = 18
	}

	bs.seen = b.Version
	bs.clampCursors()
}

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
	// CanAct rather than !Fainted: this keeps the cursor on something
	// the player may actually choose, which is a rule the server owns.
	if i >= 0 && i < len(team) && battleclient.CanAct(team[i]) {
		return i
	}
	for j, p := range team {
		if battleclient.CanAct(p) {
			return j
		}
	}
	return 0
}

func (m model) startBattle() tea.Cmd {
	team := append([]string(nil), m.team...)
	id, bc := m.joining, m.bc
	return func() tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		var (
			b   *api.Battle
			err error
		)
		if id == "" {
			b, err = bc.Create(ctx, team)
		} else {
			b, err = bc.Join(ctx, id, team)
		}
		return startedMsg{battle: b, err: err}
	}
}

func (m model) resumeBattle() tea.Cmd {
	id, bc := m.lastBattle, m.bc
	return func() tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		b, err := bc.Get(ctx, id)
		return startedMsg{battle: b, err: err}
	}
}

// startedMsg is the result of opening or joining.
type startedMsg struct {
	battle *api.Battle
	err    error
}

func (m model) attack() tea.Cmd {
	bs := m.battle
	bc := m.bc
	id, a, mv, t := bs.id, bs.pickAttacker, bs.pickMove, bs.pickTarget
	return func() tea.Msg {
		ctx, cancel := shortCtx()
		defer cancel()
		b, err := bc.Attack(ctx, id, a, mv, t)
		return battleMsg{battle: b, err: err}
	}
}
