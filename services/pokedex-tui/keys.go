package main

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func shortCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func (m model) browseKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "enter", "\r":
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
		if len(m.team) < battleclient.TeamSize {
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
		if m.bc == nil {
			m.status = "no trainer registered — run `pokedex-cli register <name>` first"
			return m, nil
		}
		m.screen = screenLobby
		m.status = ""
		return m, fetchLobby(m.bc)

	case "ctrl+r":
		if m.bc == nil {
			m.status = "no trainer registered — run `pokedex-cli register <name>` first"
			return m, nil
		}
		m.joining = ""
		m.status = "opening a battle…"
		return m, m.startBattle()

	case "g":
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
		if it, ok := m.list.SelectedItem().(item); ok && len(m.team) < battleclient.TeamSize {
			m.team = append(m.team, it.p.Name)
		}
		if len(m.team) == battleclient.TeamSize {
			return m, m.startBattle()
		}
		return m, nil
	case "tab":
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
		m.screen = screenLobby
		m.battle = nil
		// Clear it, like every other transition into the lobby does.
		// An error raised in the battle - a refused turn, say - used to
		// follow the player out and render under the lobby.
		m.status = ""
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
			bs.pickAttacker = prevPick(mine.Team, bs.pickAttacker, battleclient.CanAct)
			bs.pickMove = 0
		case focusTarget:
			bs.pickTarget = prevPick(theirs.Team, bs.pickTarget, battleclient.CanBeTargeted)
		}
	case "down", "j":
		switch bs.focus {
		case focusAttacker:
			bs.pickAttacker = nextPick(mine.Team, bs.pickAttacker, battleclient.CanAct)
			bs.pickMove = 0
		case focusTarget:
			bs.pickTarget = nextPick(theirs.Team, bs.pickTarget, battleclient.CanBeTargeted)
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
		turn := battleclient.Turn{
			Attacker: bs.pickAttacker,
			Move:     bs.pickMove,
			Target:   bs.pickTarget,
		}
		if err := m.bc.CheckTurn(bs.battle, turn); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.attack()
	}
	return m, nil
}

// nextPick and prevPick move a cursor to the next Pokemon it may
// select, using the server's own answer for what that means.
//
// The predicate is a parameter because the two cursors ask DIFFERENT
// questions, and using one for both broke the target cursor
// completely. The server publishes
//
//	CanAct:        sideActive && c.canAct()
//	CanBeTargeted: !sideActive && c.canBeTargeted()
//
// so the two are mutually exclusive: every Pokemon on the opposing
// side has CanAct false, always. A target cursor gated on CanAct
// found nothing selectable and never moved - you could not attack the
// opponent's second or third Pokemon at all.
//
// Either way it is the server's verdict rather than !Fainted, which
// is the input the server used and would be a second copy of the rule
// here.
func nextPick(team []api.BattlePokemon, i int, selectable func(api.BattlePokemon) bool) int {
	for step := 1; step <= len(team); step++ {
		j := (i + step) % len(team)
		if selectable(team[j]) {
			return j
		}
	}
	return i
}

func prevPick(team []api.BattlePokemon, i int, selectable func(api.BattlePokemon) bool) int {
	for step := 1; step <= len(team); step++ {
		j := (i - step + len(team)*2) % len(team)
		if selectable(team[j]) {
			return j
		}
	}
	return i
}
