// Package battletext is the wording Go terminal clients show for battle
// state: the glyphs, labels and advice a CLI and a TUI should not word
// two different ways.
//
// Split out of battleclient, whose doc comment promised "what it does
// NOT do is render" while four of its functions returned emoji, prose
// and an English sentence. battleclient is now true to that comment: it
// deals in state and transitions. This deals in how they read.
//
// Deliberately Go-only. The web client words the same things in
// TypeScript against the same structured fields, so this is the Go half
// of a two-implementation convention, not something to grow a third
// consumer. What all three clients share is the API schema, not this.
//
// MoveUsable stayed behind in battleclient on purpose. It looks
// presentational at both call sites - greying a label, printing
// "(disabled)" - but it answers "would this be a 409?", which is a
// protocol precondition rather than a rendering.
package battletext

import (
	"errors"
	"fmt"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// StageLabel renders a Pokemon's stat changes as something short enough
// to sit beside its name: "+2 atk -1 def", or "" when nothing has moved.
//
// Here rather than in each client because both terminals want the same
// string and the ordering has to be stable - a label whose fields
// reshuffle between polls reads as flicker.
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

// Conditions are the non-stat things worth warning about before a turn:
// confusion, and a move that cannot be used.
//
// Returned as strings rather than booleans so a caller can print them
// without restating the wording, and so adding a condition later does
// not change every call site.
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

// EventIcon is a one-glyph summary of what a log line did, for clients
// that cannot animate. Returns "" for events that are pure narration.
//
// Keyed off the structured fields rather than off ev.Text: the server
// owns the wording precisely so clients do not restate it, and a client
// that greps prose for "super effective" breaks the first time that
// sentence is reworded. effectiveness, damage and fainted are on the
// event for exactly this.
//
// Shared with the TUI so the same turn does not get two different
// glyphs depending on which terminal is watching.
func EventIcon(ev api.BattleEvent) string {
	if f, ok := ev.Fainted.Get(); ok && f {
		return "💀"
	}
	// Effectiveness before damage: a 2x hit and an ordinary hit are both
	// damage, and which one it was is the thing worth a glyph.
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
	// A move with no damage and no multiplier is a status move; it
	// still did something, which is the whole point of them existing.
	if _, ok := ev.Move.Get(); ok {
		return "✨"
	}
	return ""
}

// IdentityAdvice turns an identity failure into what the player should
// do about it, or "" for any other error.
//
// Shared because all three clients hit the same wall: trainers live in
// the server's memory, so every deploy invalidates every stored token,
// and the raw message ("unknown trainer token") names the problem
// without naming the fix.
//
// This only explains. Clearing the stored token is the caller's, since
// a CLI that exits wants the file gone while a running TUI wants to
// re-register in place - the same advice, different mechanics.
func IdentityAdvice(err error) string {
	switch {
	case errors.Is(err, battleclient.ErrNoIdentity):
		return "no trainer registered yet — register to start battling"
	case errors.Is(err, battleclient.ErrStaleIdentity):
		return "the server no longer knows this trainer (it restarted) — register again"
	}
	return ""
}
