package main

import (
	"strconv"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

type screen int

const (
	screenRegister screen = iota
	screenBrowse
	screenLobby
	screenTeam
	screenBattle
)

func (s screen) String() string {
	switch s {
	case screenRegister:
		return "register"
	case screenLobby:
		return "lobby"
	case screenTeam:
		return "team"
	case screenBattle:
		return "battle"
	default:
		return "browse"
	}
}

// teamSize is the server's team count; this constant only drives when Start lights up.
const teamSize = 3

// toggleTeam adds or removes a name; the server reads team as an ordered list, so pick order is preserved.
func toggleTeam(team []string, name string) []string {
	for i, n := range team {
		if n == name {
			return append(append([]string{}, team[:i]...), team[i+1:]...)
		}
	}
	if len(team) >= teamSize {
		return team
	}
	return append(append([]string{}, team...), name)
}

func teamReady(team []string) bool { return len(team) == teamSize }

// teamPosition returns the 1-based pick order, or 0 when not picked.
func teamPosition(team []string, name string) int {
	for i, n := range team {
		if n == name {
			return i + 1
		}
	}
	return 0
}

func summarise(p api.BattlePokemon) string {
	parts := []string{p.Name}
	if s := battletext.StageLabel(p); s != "" {
		parts = append(parts, s)
	}
	if c := battletext.Conditions(p); len(c) > 0 {
		parts = append(parts, strings.Join(c, " "))
	}
	return strings.Join(parts, "  ")
}

// hpFraction is the health bar's fill, clamped to [0,1] so a server reporting hp>maxHp cannot draw past the track.
func hpFraction(p api.BattlePokemon) float32 {
	if p.MaxHp <= 0 {
		return 0
	}
	f := float32(p.Hp) / float32(p.MaxHp)
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	}
	return f
}

// moveLabel formats a move button; power 0 renders as a dash so a status move does not look like a bug.
func moveLabel(m api.Move) string {
	if m.Power == 0 {
		return title(m.Name) + "  ·  —"
	}
	return title(m.Name) + "  ·  " + strconv.Itoa(m.Power)
}

// canAttack reports whether a move button should be tappable; uses battleclient's CanAct/CanBeTargeted, not !Fainted — the server owns the rule and a derived copy will drift.
func canAttack(b *api.Battle, bc *battleclient.Client, mon api.BattlePokemon, moveIdx int) bool {
	if b == nil || bc == nil {
		return false
	}
	if !bc.MyTurn(b) {
		return false
	}
	if !battleclient.CanAct(mon) {
		return false
	}
	return battleclient.MoveUsable(mon, moveIdx)
}

func turnBanner(b *api.Battle, bc *battleclient.Client) string {
	if b == nil {
		return ""
	}
	if b.Status == "finished" {
		if w := b.Winner.Value; w != "" {
			if bc != nil && w == bc.Name {
				return "You win!"
			}
			return title(w) + " wins"
		}
		return "Battle over"
	}
	if bc != nil && bc.MyTurn(b) {
		return "your turn"
	}
	return "waiting on " + b.Turn.Value
}

// recentLog returns the last n battle events, newest last, so the log cannot push the controls off screen.
func recentLog(b *api.Battle, n int) []api.BattleEvent {
	if b == nil || len(b.Log) == 0 {
		return nil
	}
	if len(b.Log) <= n {
		return b.Log
	}
	return b.Log[len(b.Log)-n:]
}

// eventLine renders one log entry; kind comes from shared battletext, glyph is chosen here since a phone is not a terminal.
func eventLine(ev api.BattleEvent) string {
	if icon := eventGlyph(battletext.Classify(ev)); icon != "" {
		return icon + "  " + ev.Text
	}
	return ev.Text
}

func eventGlyph(k battletext.EventKind) string {
	switch k {
	case battletext.EventFainted:
		return "\u2620\ufe0f" // skull and crossbones
	case battletext.EventNoEffect:
		return "\u26d4" // no entry
	case battletext.EventSuperEffective:
		return "\u2757" // exclamation
	case battletext.EventResisted:
		return "\U0001f6e1\ufe0f" // shield
	case battletext.EventHit:
		return "\U0001f44a" // fist
	case battletext.EventStatus:
		return "\u2728" // sparkles
	}
	return ""
}

// pick is a turn-in-progress: (attacker, move, target) with flags tracking how far the three taps have got.
type pick struct {
	attacker int
	move     int
	target   int

	haveAttacker bool
	haveMove     bool
}

// reset clears the selection; called after every server update since old indices may not point at a living Pokemon.
func (p *pick) reset() { *p = pick{} }

func (p *pick) back() {
	switch {
	case p.haveMove:
		p.haveMove = false
	case p.haveAttacker:
		p.haveAttacker = false
	}
}

func (p pick) canGoBack() bool { return p.haveAttacker || p.haveMove }

func (p pick) stage() string {
	switch {
	case !p.haveAttacker:
		return "pick a pokemon"
	case !p.haveMove:
		return "pick a move"
	default:
		return "pick a target"
	}
}

func (p pick) ready() bool { return p.haveAttacker && p.haveMove }

// aliveIndexes returns team positions (not renumbered) that can still act; uses battleclient.CanAct so we track the server's rule, not our own copy of it.
func aliveIndexes(side api.Side) []int {
	var out []int
	for i, m := range side.Team {
		if battleclient.CanBeTargeted(m) {
			out = append(out, i)
		}
	}
	return out
}

func defaultTarget(theirs api.Side) int {
	if alive := aliveIndexes(theirs); len(alive) > 0 {
		return alive[0]
	}
	return 0
}

func onlyOneTarget(theirs api.Side) bool {
	return len(aliveIndexes(theirs)) == 1
}

func (a *ui) filteredDex() []api.Pokemon {
	q := strings.ToLower(strings.TrimSpace(a.filter.Text()))
	if q == "" {
		return a.dex
	}
	var out []api.Pokemon
	for _, p := range a.dex {
		hay := strings.ToLower(p.Name + " " + strings.Join(p.Types, " "))
		if strings.Contains(hay, q) {
			out = append(out, p)
		}
	}
	return out
}

// badTrainerName rejects names the server would accept but the player cannot rename; empty return means ok.
func badTrainerName(s string) string {
	switch {
	case s == "":
		return "Enter a name."
	case len([]rune(s)) < 2:
		return "That is a bit short."
	case len([]rune(s)) > 20:
		return "Keep it under 20 characters."
	}
	for _, r := range s {
		ok := r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !ok {
			return "Letters, numbers, - and _ only."
		}
	}
	return ""
}
