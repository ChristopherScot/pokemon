package battletext

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func TestEventIconsAreOneCodePointWide(t *testing.T) {
	events := []api.BattleEvent{
		{Fainted: api.NewOptBool(true)},
		{Effectiveness: api.NewOptFloat64(0)},
		{Effectiveness: api.NewOptFloat64(2)},
		{Effectiveness: api.NewOptFloat64(0.5)},
		{Damage: api.NewOptInt(30)},
		{Move: api.NewOptString("harden")},
	}
	seen := map[string]bool{}
	for _, ev := range events {
		icon := EventIcon(ev)
		if icon == "" {
			t.Errorf("%+v: no glyph", ev)
			continue
		}
		if n := utf8.RuneCountInString(icon); n != 1 {
			t.Errorf("%q is %d runes; a multi-rune glyph is the width bug", icon, n)
		}
		if strings.ContainsRune(icon, '️') {
			t.Errorf("%q carries a variation selector", icon)
		}
		seen[icon] = true
	}
	// Distinct glyphs, or the column carries no information.
	if len(seen) != len(events) {
		t.Errorf("got %d distinct glyphs for %d kinds of event: %v",
			len(seen), len(events), seen)
	}
}

func TestEventIconIsEmptyForNarration(t *testing.T) {
	if icon := EventIcon(api.BattleEvent{TurnNumber: 3, Text: "Turn 3"}); icon != "" {
		t.Errorf("narration got glyph %q", icon)
	}
}

func TestIdentityAdviceOnlyAnswersIdentityErrors(t *testing.T) {
	if IdentityAdvice(battleclient.ErrStaleIdentity) == "" {
		t.Error("no advice for a stale identity")
	}
	if IdentityAdvice(battleclient.ErrNoIdentity) == "" {
		t.Error("no advice for a missing identity")
	}
	if got := IdentityAdvice(errors.New("connection refused")); got != "" {
		t.Errorf("advice for an unrelated error: %q", got)
	}
}
