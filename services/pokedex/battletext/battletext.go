// Package battletext is the wording Go terminal clients show for battle
package battletext

import (
	"errors"
	"fmt"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
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

func EventIcon(ev api.BattleEvent) string {
	if f, ok := ev.Fainted.Get(); ok && f {
		return "💀"
	}
	if e, ok := ev.Effectiveness.Get(); ok {
		switch {
		case e == 0:
			return "🚫" // immune
		case e >= 2:
			return "💥"
		case e > 0 && e < 1:
			return "🪨"
		}
	}
	if d, ok := ev.Damage.Get(); ok && d > 0 {
		return "👊"
	}
	if _, ok := ev.Move.Get(); ok {
		return "✨"
	}
	return ""
}

func IdentityAdvice(err error) string {
	switch {
	case errors.Is(err, battleclient.ErrNoIdentity):
		return "no trainer registered yet — register to start battling"
	case errors.Is(err, battleclient.ErrStaleIdentity):
		return "the server no longer knows this trainer (it restarted) — register again"
	}
	return ""
}
