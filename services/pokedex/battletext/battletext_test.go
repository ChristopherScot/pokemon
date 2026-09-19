package battletext

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// The glyph column has to be one width for every event, or the prose
// beside it steps left and right as the log scrolls.
//
// A variation selector (U+FE0F) is the way this breaks: emoji that
// carry one render two columns in some terminals and one in others,
// while a plain emoji-presentation code point is two everywhere. The
// first version of EventIcon used U+FE0F on two of its six glyphs.
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

// Narration - a turn header, a battle-start line - has no glyph, and
// must not be given one, or every line in the log is decorated and the
// column stops meaning anything.
func TestEventIconIsEmptyForNarration(t *testing.T) {
	if icon := EventIcon(api.BattleEvent{TurnNumber: 3, Text: "Turn 3"}); icon != "" {
		t.Errorf("narration got glyph %q", icon)
	}
}

// A 401 from the server has to arrive as ErrStaleIdentity, not as a
// bare message.
//
// Each Unauthorized case used to become errors.New(v.Message), which
// threw away the one fact a caller needs: that this is an identity
// problem with a known fix. Clients then printed "unknown trainer
// token; register first" and cleared nothing, so every later command
// failed the same way.

// Advice is offered for an identity failure and withheld for anything
// else, so a caller can print it unconditionally.
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
