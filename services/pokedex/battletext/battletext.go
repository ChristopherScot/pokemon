// Package battletext is the wording every Go client narrates a battle with.
// Classifying an event lives here; rendering it (glyph, drawable, CSS class)
// lives in the client. EventKind is the seam.
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
type EventKind int

const (
	EventPlain EventKind = iota
	EventFainted
	EventNoEffect
	EventSuperEffective
	EventResisted
	EventHit
	EventStatus
)

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

// EventIcon is the terminal rendering; non-terminal clients call Classify
// and map the kind themselves.
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
