package main

// Per-screen key handling. One function per screen, dispatched from
// handleKey, so a binding that means one thing while browsing and
// another mid-battle is written once in each place rather than guarded
// by a chain of conditions.

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// shortCtx bounds a request. Battle actions are interactive: one that
// has not answered in a few seconds should say so rather than hang.
func shortCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (m model) browseKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "enter", "\r":
		// Toggle the highlighted Pokemon on the team, here on the
		// browse screen rather than behind a separate picker. This is
		// the list you are already looking at, and hiding selection
		// three screens away made enter look broken.
		it, ok := m.list.SelectedItem().(item)
		if !ok {
			return m, nil
		}
		for i, n := range m.team {
			if n == it.p.Name {
				m.team = append(m.team[:i], m.team[i+1:]...)
				return m, nil
			}
		}
		if len(m.team) < 3 {
			m.team = append(m.team, it.p.Name)
		} else {
			m.status = "three is a full team — press enter on one to drop it"
		}
		return m, nil

	case "backspace":
		if n := len(m.team); n > 0 {
			m.team = m.team[:n-1]
		}
		return m, nil

	case "b":
		// Battle mode needs a trainer; say so rather than opening a
		// lobby that cannot do anything.
		if m.bc == nil {
			m.status = "no trainer registered — run `pokedex-cli register <name>` first"
			return m, nil
		}
		m.screen = screenLobby
		m.status = ""
		return m, fetchLobby(m.bc)

	case "ctrl+r":
		// Open a battle with whatever is picked. Anything left out is
		// chosen by the server, so this works with an empty team too.
		if m.bc == nil {
			m.status = "no trainer registered — run `pokedex-cli register <name>` first"
			return m, nil
		}
		m.joining = ""
		m.status = "opening a battle…"
		return m, m.startBattle()

	case "g":
		// Back to the battle in progress. Browsing the pokedex
		// mid-battle is normal - checking what a move does, or what
		// the opponent is weak to - and esc was a one-way door.
		if m.lastBattle == "" {
			m.status = "no battle to go back to"
			return m, nil
		}
		m.status = "resuming " + m.lastBattle + "…"
		return m, m.resumeBattle()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) lobbyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.screen = screenBrowse
		m.status = ""
		return m, nil
	case "r":
		return m, fetchLobby(m.bc)
	case "g":
		// Back into the battle you walked out of. Nothing else can get
		// you there: the list below holds only battles still waiting.
		if m.lastBattle == "" {
			m.status = "no battle to go back to"
			return m, nil
		}
		m.status = "resuming " + m.lastBattle + "..."
		return m, m.resumeBattle()
	case "up", "k":
		if m.lobbyIdx > 0 {
			m.lobbyIdx--
		}
		return m, nil
	case "down", "j":
		if m.lobby != nil && m.lobbyIdx < len(m.lobby.Waiting)-1 {
			m.lobbyIdx++
		}
		return m, nil
	case "n":
		// Open a new battle: pick a team first.
		m.screen = screenTeam
		m.joining = ""
		m.team = nil
		m.status = ""
		return m, nil
	case "enter", "\r":
		if m.lobby == nil || len(m.lobby.Waiting) == 0 {
			return m, nil
		}
		m.screen = screenTeam
		m.joining = m.lobby.Waiting[m.lobbyIdx].BattleId
		m.team = nil
		m.status = ""
		return m, nil
	}
	return m, nil
}

// teamKey picks three Pokemon from the browse list, reusing the list the
// player already knows rather than inventing a second picker.
func (m model) teamKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenLobby
		m.team = nil
		m.status = ""
		return m, nil
	case "backspace":
		if n := len(m.team); n > 0 {
			m.team = m.team[:n-1]
		}
		return m, nil
	case "enter", "\r":
		if it, ok := m.list.SelectedItem().(item); ok && len(m.team) < 3 {
			m.team = append(m.team, it.p.Name)
		}
		if len(m.team) == 3 {
			return m, m.startBattle()
		}
		return m, nil
	case "tab":
		// Start with fewer than three; the server fills the rest.
		//
		// Without this the picker could only start on a full team, so
		// the empty slots' die was decoration - the API has taken a
		// partial team since 0.4.0 and the web has offered it since,
		// but a TUI player had to name all three or go back.
		return m, m.startBattle()
	}
	// Everything else drives the list, so filtering works while picking.
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) battleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	bs := m.battle
	if bs == nil {
		m.screen = screenBrowse
		return m, nil
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		// Leaving the screen does not forfeit: the battle stays open and
		// can be rejoined from the lobby.
		m.screen = screenLobby
		m.battle = nil
		return m, fetchLobby(m.bc)
	}

	// Nothing below changes anything unless it is this player's move.
	if bs.battle == nil || !m.bc.MyTurn(bs.battle) {
		return m, nil
	}
	mine, theirs, ok := m.bc.SideFor(bs.battle)
	if !ok {
		return m, nil
	}

	switch msg.String() {
	case "tab":
		bs.focus = (bs.focus + 1) % 3
	case "shift+tab":
		bs.focus = (bs.focus + 2) % 3
	case "up", "k":
		switch bs.focus {
		case focusAttacker:
			bs.pickAttacker = prevAlive(mine.Team, bs.pickAttacker)
			bs.pickMove = 0
		case focusTarget:
			bs.pickTarget = prevAlive(theirs.Team, bs.pickTarget)
		}
	case "down", "j":
		switch bs.focus {
		case focusAttacker:
			bs.pickAttacker = nextAlive(mine.Team, bs.pickAttacker)
			bs.pickMove = 0
		case focusTarget:
			bs.pickTarget = nextAlive(theirs.Team, bs.pickTarget)
		}
	case "left", "h":
		if bs.pickMove > 0 {
			bs.pickMove--
		}
	case "right", "l":
		if n := len(mine.Team[bs.pickAttacker].Moves); bs.pickMove < n-1 {
			bs.pickMove++
		}
	case "enter", "\r":
		return m, m.attack()
	}
	return m, nil
}

// nextAlive and prevAlive skip fainted Pokemon, so the cursor never
// lands somewhere that cannot act.
func nextAlive(team []api.BattlePokemon, i int) int {
	for step := 1; step <= len(team); step++ {
		j := (i + step) % len(team)
		if !team[j].Fainted {
			return j
		}
	}
	return i
}

func prevAlive(team []api.BattlePokemon, i int) int {
	for step := 1; step <= len(team); step++ {
		j := (i - step + len(team)*2) % len(team)
		if !team[j].Fainted {
			return j
		}
	}
	return i
}
