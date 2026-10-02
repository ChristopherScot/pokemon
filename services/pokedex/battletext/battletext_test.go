package battletext

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/christopherscot/pokemon/services/pokedex/api"
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

func TestClassifyNamesWhatHappenedWithoutChoosingAGlyph(t *testing.T) {
	for name, tc := range map[string]struct {
		ev   api.BattleEvent
		want EventKind
	}{
		"fainted":         {api.BattleEvent{Fainted: api.NewOptBool(true)}, EventFainted},
		"immune":          {api.BattleEvent{Effectiveness: api.NewOptFloat64(0)}, EventNoEffect},
		"super effective": {api.BattleEvent{Effectiveness: api.NewOptFloat64(2)}, EventSuperEffective},
		"resisted":        {api.BattleEvent{Effectiveness: api.NewOptFloat64(0.5)}, EventResisted},
		"plain hit":       {api.BattleEvent{Damage: api.NewOptInt(10)}, EventHit},
		"status move":     {api.BattleEvent{Move: api.NewOptString("growl")}, EventStatus},
		"nothing":         {api.BattleEvent{Text: "a new battle"}, EventPlain},
	} {
		if got := Classify(tc.ev); got != tc.want {
			t.Errorf("Classify(%s) = %v, want %v", name, got, tc.want)
		}
	}
}

// EventIcon must keep agreeing with Classify, or terminals and phone
// narrate the same event differently.
func TestEventIconFollowsClassify(t *testing.T) {
	fainted := api.BattleEvent{Fainted: api.NewOptBool(true)}
	if Classify(fainted) != EventFainted {
		t.Fatal("fixture no longer classifies as fainted")
	}
	if EventIcon(fainted) == "" {
		t.Error("a classified event rendered no icon")
	}
	if EventIcon(api.BattleEvent{Text: "plain"}) != "" {
		t.Error("an unclassified event rendered an icon")
	}
}
