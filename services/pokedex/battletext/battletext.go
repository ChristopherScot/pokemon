// Package battletext is the wording Go clients show for a battle, so
// that a terminal, a phone and a web page narrate it the same way.
//
// The doc used to say "terminal clients", which stopped being true
// when the Android app imported it - and the phone then rendered
// terminal emoji, because emoji was what the package offered.
//
// The split this package now keeps: CLASSIFYING an event is general
// and lives here; choosing a glyph for it is presentation and lives
// in the client. EventKind is the seam.
package battletext

import (
	"fmt"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func StageLabel(p api.BattlePokemon) string {
	st, ok := p.Stages.Get()
	if !ok {
		return ""
	}
	var parts []string
	for _, f := range []struct {
		name string
		val  api.OptInt
	}{
		{"atk", st.Attack},
		{"def", st.Defense},
		{"spd", st.Speed},
		{"acc", st.Accuracy},
	} {
		if v, ok := f.val.Get(); ok && v != 0 {
			parts = append(parts, fmt.Sprintf("%+d %s", v, f.name))
		}
	}
	return strings.Join(parts, " ")
}

func Conditions(p api.BattlePokemon) []string {
	var out []string
	if c, ok := p.Confused.Get(); ok && c {
		out = append(out, "confused")
	}
	if i, ok := p.DisabledMove.Get(); ok && i >= 0 && i < len(p.Moves) {
		out = append(out, "disabled: "+p.Moves[i].Name)
	}
	return out
}

// EventKind is what happened, without saying how to show it.
//
// A terminal wants an emoji, an Android app wants a drawable and a
// web page wants a CSS class. The classification is the same for all
// three; only the rendering differs, so only the rendering belongs
// in a client.
type EventKind int

const (
	// EventPlain is an event with nothing to mark - a log line.
	EventPlain EventKind = iota
	EventFainted
	EventNoEffect
	EventSuperEffective
	EventResisted
	EventHit
	EventStatus
)

// Classify reports what kind of event this is.
func Classify(ev api.BattleEvent) EventKind {
	if f, ok := ev.Fainted.Get(); ok && f {
		return EventFainted
	}
	if e, ok := ev.Effectiveness.Get(); ok {
		switch {
		case e == 0:
			return EventNoEffect
		case e >= 2:
			return EventSuperEffective
		case e > 0 && e < 1:
			return EventResisted
		}
	}
	if d, ok := ev.Damage.Get(); ok && d > 0 {
		return EventHit
	}
	if _, ok := ev.Move.Get(); ok {
		return EventStatus
	}
	return EventPlain
}

// EventIcon is the terminal rendering of an event kind.
//
// Kept here because the CLI and the TUI both want exactly this table
// and neither is a better home than the other. A client that wants
// something else - a drawable, a CSS class - calls Classify and maps
// the kind itself, which is what the phone now does.
func EventIcon(ev api.BattleEvent) string {
	switch Classify(ev) {
	case EventFainted:
		return "💀"
	case EventNoEffect:
		return "🚫"
	case EventSuperEffective:
		return "💥"
	case EventResisted:
		return "🪨"
	case EventHit:
		return "👊"
	case EventStatus:
		return "✨"
	}
	return ""
}
