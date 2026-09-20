package main

// The app's state machine and every decision about WHAT to show.
//
// Nothing here touches layout.Context, so `go test ./...` covers it on
// any machine - a CI runner has no display, and a test that needs a
// Gio frame is an integration test with a device in it.
//
// The wording comes from battletext, shared with the CLI and TUI, so
// three clients narrate a battle identically rather than each inventing
// phrasing that drifts apart.

import (
	"strconv"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

// screen mirrors the TUI's screens, plus one the terminal does not need:
// a phone has no `pokedex-cli register`, so the app has to be able to
// create a trainer itself.
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

// teamSize is how many Pokemon a battle takes. The server decides this;
// the constant here only drives when the Start button lights up.
const teamSize = 3

// toggleTeam adds or removes a name, preserving pick order.
//
// Order matters: the server reads the team as a list, so the first pick
// leads. Removing from the middle keeps the rest in the order they were
// chosen rather than resorting them.
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

// teamReady reports whether the team can start a battle.
func teamReady(team []string) bool { return len(team) == teamSize }

// teamPosition returns the 1-based pick order, or 0 when not picked.
// The UI shows this instead of a checkmark so the lead is visible.
func teamPosition(team []string, name string) int {
	for i, n := range team {
		if n == name {
			return i + 1
		}
	}
	return 0
}

// summarise renders one Pokemon the way the CLI and TUI render it.
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

// hpFraction is the health bar's fill, clamped to [0,1].
//
// Clamped because a server that reports hp above maxHp - a heal, a bug -
// should not draw a bar past the end of its track.
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

// moveLabel is the text on a move button.
//
// Power reads as a dash for status moves, matching the other clients:
// a status move has power 0, and "0" looks like a bug rather than a
// deliberate absence.
func moveLabel(m api.Move) string {
	if m.Power == 0 {
		return title(m.Name) + "  ·  —"
	}
	return title(m.Name) + "  ·  " + strconv.Itoa(m.Power)
}

// canAttack reports whether a move button should be tappable.
//
// Every question here is answered by the server and read through
// battleclient - the phone holds no rules of its own. A client that
// decided for itself would disagree with the server the first time a
// rule changed, and the player would find out by having a tap
// rejected with a 409.
func canAttack(b *api.Battle, bc *battleclient.Client, mon api.BattlePokemon, moveIdx int) bool {
	if b == nil || bc == nil {
		return false
	}
	if !bc.MyTurn(b) {
		return false
	}
	// CanAct rather than !Fainted: fainted is the INPUT the server
	// used, and reading it here would be re-deriving a conclusion the
	// server has already published.
	if !battleclient.CanAct(mon) {
		return false
	}
	return battleclient.MoveUsable(mon, moveIdx)
}

// turnBanner is the line above the battle, which on a phone is the only
// always-visible place to say whose turn it is.
func turnBanner(b *api.Battle, bc *battleclient.Client) string {
	if b == nil {
		return ""
	}
	if b.Status == "finished" {
		// The API has carried a winner all along. Ending a battle with
		// "battle over" and leaving the player to infer it from the HP
		// bars was the worst miss in the first version.
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

// recentLog returns the last n battle events, newest last.
//
// A phone shows fewer lines than a terminal, so this trims rather than
// letting the log push the controls off the screen.
func recentLog(b *api.Battle, n int) []api.BattleEvent {
	if b == nil || len(b.Log) == 0 {
		return nil
	}
	if len(b.Log) <= n {
		return b.Log
	}
	return b.Log[len(b.Log)-n:]
}

// eventLine is one rendered log entry.
//
// The KIND comes from battletext, shared with the terminal clients so
// all of them agree on what happened. The glyph is chosen here,
// because that is a presentation decision and a phone is not a
// terminal - it used to call EventIcon and render the terminal's
// emoji, which is what made a package documented for "terminal
// clients" the phone's renderer too.
func eventLine(ev api.BattleEvent) string {
	if icon := eventGlyph(battletext.Classify(ev)); icon != "" {
		return icon + "  " + ev.Text
	}
	return ev.Text
}

// eventGlyph is this app's rendering of an event kind.
//
// Still emoji today, and deliberately its own table: swapping in a
// drawable is a change to this function and nothing else.
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

// --- turn selection -------------------------------------------------
//
// A turn is (attacker, move, target). The terminal moves a cursor
// between three columns; a phone taps the thing it means, so the state
// here is "what has been tapped so far" rather than "where is focus".

type pick struct {
	attacker int
	move     int
	target   int

	// chosen tracks how far through the three taps we are, so the UI
	// can highlight the next thing to pick rather than showing three
	// equally-live columns on a narrow screen.
	haveAttacker bool
	haveMove     bool
}

// reset clears a selection, which happens after a turn resolves and
// whenever the battle state arrives from the server - the previous
// indices may no longer point at a living Pokemon.
func (p *pick) reset() { *p = pick{} }

// back undoes the last stage. Without it a mis-tap on the attacker is
// unrecoverable: the player has to finish a turn they did not want,
// which on a phone is a thumb away at all times.
func (p *pick) back() {
	switch {
	case p.haveMove:
		p.haveMove = false
	case p.haveAttacker:
		p.haveAttacker = false
	}
}

// canGoBack reports whether there is a stage to undo.
func (p pick) canGoBack() bool { return p.haveAttacker || p.haveMove }

// stage names what the player should tap next. The banner shows this,
// because on a phone there is no room for three labelled columns.
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

// ready reports whether a full turn has been selected.
func (p pick) ready() bool { return p.haveAttacker && p.haveMove }

// aliveIndexes lists the positions in a team that can still act.
//
// Returned as indexes rather than values because the server addresses
// Pokemon by position, so the UI must not renumber them when one faints.
func aliveIndexes(side api.Side) []int {
	var out []int
	for i, m := range side.Team {
		// CanBeTargeted rather than !Fainted. This drives which
		// targets are offered and whether the third tap can be
		// skipped, so it is a rule - and a client that derives it
		// from fainted has a copy that drifts.
		if battleclient.CanBeTargeted(m) {
			out = append(out, i)
		}
	}
	return out
}

// defaultTarget is the first living opponent, so a player who taps a
// move gets a sensible target without a third tap in the common case
// where only one is left.
func defaultTarget(theirs api.Side) int {
	if alive := aliveIndexes(theirs); len(alive) > 0 {
		return alive[0]
	}
	return 0
}

// onlyOneTarget reports whether the target tap can be skipped.
func onlyOneTarget(theirs api.Side) bool {
	return len(aliveIndexes(theirs)) == 1
}

// filteredDex applies the search box.
//
// Matches name and type, the same fields the TUI's filter uses, so a
// search that works in the terminal works here.
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

// badTrainerName explains why a name will not do, or returns "".
//
// The server accepts almost anything - it does not even require the
// name to be unique - so a stray thumb produces a trainer called
// "test2637_74(4" and there is no way to rename one. Checked here
// because this is the only place it can be checked before it sticks.
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
