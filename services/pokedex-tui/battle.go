package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

const frameRate = time.Second / 30

const drainPerFrame = 4

// frameMsg advances animations.
type frameMsg time.Time

// battleMsg carries new state from a poll.
type battleMsg struct {
	// polled distinguishes a background poll from the result of an
	// action the player took, the same way lobbyMsg does.
	//
	// Without it the handler could not tell them apart and re-armed
	// pollBattle on every battleMsg - so each attack started a SECOND
	// poll chain on top of the one already running, and they never
	// merged or stopped. Ten turns meant eleven GETs a second against
	// the API, growing for as long as the battle lasted.
	polled bool

	// id is the battle this reply is about.
	//
	// A tea.Cmd cannot be cancelled, so leaving a battle does not stop
	// its poll - the reply still arrives a second later. The handler
	// only checked that SOME battle was open, so joining a new one
	// inside that window applied the old battle's state to the new
	// one: wrong HP, wrong teams, wrong log, under the new battle's
	// id. Worse, the foreign Version overwrote `seen`, so real updates
	// at lower versions stopped animating for the rest of the game.
	id string

	battle *api.Battle
	err    error
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
	// kind is battletext.Classify's answer, not the raw effectiveness.
	//
	// The TUI used to re-derive "super effective" / "resisted" / "no
	// effect" from the float at four separate sites, and the copies
	// had drifted: two guarded the resisted case with e > 0 && e < 1
	// and two used a bare e < 1, and none of them knew about fainting
	// at all - so a killing super-effective blow showed the skull icon
	// from EventIcon and super-effective styling from the local
	// ladder, in the same row.
	kind battletext.EventKind
	// frames remaining; the float rises and fades as this counts down.
	life int
}

type impact struct {
	slot slot
	kind battletext.EventKind
	life int
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
		return battleMsg{polled: true, id: id, battle: b, err: err}
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
				// One classification, from the package that owns it,
				// used by every part of the row. faintLife comes from
				// the same answer rather than a second read of
				// ev.Fainted.
				kind := battletext.Classify(ev)
				bs.floats = append(bs.floats, damageFloat{
					slot:   k,
					amount: dmg,
					kind:   kind,
					life:   floatLife,
				})
				if bs.impacts == nil {
					bs.impacts = map[slot]*impact{}
				}
				life := impactLife
				if kind == battletext.EventFainted {
					life = faintLife
				}
				bs.impacts[k] = &impact{slot: k, kind: kind, life: life}
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
			continue
		}
		// Only for an impact that survives, matching the float and
		// pulse loops. Claiming motion on the frame that deletes the
		// last one costs an extra tick and makes advance()'s "is
		// anything still moving" answer not quite true.
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
	bs.pickAttacker = clampPick(mine.Team, bs.pickAttacker, battleclient.CanAct)
	bs.pickTarget = clampPick(theirs.Team, bs.pickTarget, battleclient.CanBeTargeted)
	if bs.pickAttacker < len(mine.Team) {
		if n := len(mine.Team[bs.pickAttacker].Moves); n > 0 && bs.pickMove >= n {
			bs.pickMove = n - 1
		}
	}
}

// clampPick moves an index onto a Pokemon the cursor may select.
// Takes the predicate for the same reason nextPick does: an attacker
// and a target are selected by different rules.
func clampPick(team []api.BattlePokemon, i int, selectable func(api.BattlePokemon) bool) int {
	if len(team) == 0 {
		return 0
	}
	if i >= 0 && i < len(team) && selectable(team[i]) {
		return i
	}
	for j, p := range team {
		if selectable(p) {
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
		return battleMsg{id: id, battle: b, err: err}
	}
}
