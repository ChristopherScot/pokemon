package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

func index1(s, what string) (int, error) {
	// Atoi, not Sscanf: Sscanf stops at the first byte that does not
	// match and reports success for the prefix it consumed, so "2x",
	// "1.9" and "2 3" all parsed as numbers and the rest was dropped
	// in silence - `attack abc 1 2.9 3` used move 2 without a word.
	// Atoi rejects the whole string unless it is exactly an integer.
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", what, s)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: positions start at 1", what)
	}
	return n - 1, nil
}

func printEvent(ev api.BattleEvent) {
	if icon := battletext.EventIcon(ev); icon != "" {
		fmt.Printf(" %s %s\n", icon, ev.Text)
		return
	}
	fmt.Println("   ", ev.Text)
}

func lastTurn(b *api.Battle) []api.BattleEvent {
	if len(b.Log) == 0 {
		return nil
	}
	last := b.Log[len(b.Log)-1].TurnNumber
	var out []api.BattleEvent
	for _, ev := range b.Log {
		if ev.TurnNumber == last {
			out = append(out, ev)
		}
	}
	return out
}

func printBattle(c *battleclient.Client, b *api.Battle) {
	mine, theirs, ok := c.SideFor(b)
	if !ok {
		if len(b.Sides) < 2 {
			fmt.Printf("battle %s: waiting for an opponent\n", b.ID)
			if len(b.Sides) == 1 {
				fmt.Printf("  %s: %s\n", b.Sides[0].Trainer, teamLine(b.Sides[0]))
			}
			return
		}
		printSpectated(b)
		return
	}

	fmt.Printf("battle %s\n", b.ID)
	printSide("you", mine)
	printSide(theirs.Trainer, theirs)

	switch {
	case b.Status == api.BattleStatusFinished:
		if w, ok := b.Winner.Get(); ok {
			if w == c.Name {
				fmt.Println("\nyou win!")
			} else {
				fmt.Printf("\n%s wins.\n", w)
			}
		} else {
			fmt.Println("\nthe battle is a draw.")
		}
	case c.MyTurn(b):
		fmt.Println("\nyour moves")
		// The first pokemon that can actually act, which is what the
		// usage hint below has to describe. It used to read
		// mine.Team[0] unconditionally - on the same line that
		// guarded len(mine.Team) - so an empty team panicked, and a
		// team whose slot 0 had a different move count printed the
		// wrong range.
		firstUsable := -1
		for i, p := range mine.Team {
			// CanAct, not !Fainted: this decides what the player is
			// OFFERED, which is a rule, and the server owns the
			// rules. The "(fainted)" label further down is
			// presentation and correctly still reads Fainted.
			if !battleclient.CanAct(p) {
				continue
			}
			if firstUsable < 0 {
				firstUsable = i
			}
			fmt.Printf("  %d %s\n", i+1, p.Name)
			for j, mv := range p.Moves {
				power := fmt.Sprintf("%d", mv.Power)
				if mv.Power == 0 {
					power = "status"
				}
				note := ""
				if !battleclient.MoveUsable(p, j) {
					note = "  (disabled)"
				}
				fmt.Printf("      %d %-16s %-9s %s%s\n", j+1, mv.Name, mv.Type, power, note)
			}
		}
		// Only when there IS something to offer: with no usable
		// pokemon the hint would describe a move nobody can make.
		if firstUsable >= 0 {
			fmt.Printf("\nyour move: pokedex-cli attack %s <your 1-%d> <move 1-%d> <their 1-%d>\n",
				b.ID, len(mine.Team), len(mine.Team[firstUsable].Moves), len(theirs.Team))
		}
	default:
		fmt.Printf("\nwaiting on %s. `watch %s` to block until it is your turn.\n", b.Turn.Value, b.ID)
	}
}

func printSpectated(b *api.Battle) {
	fmt.Printf("battle %s (watching)\n", b.ID)
	for _, s := range b.Sides {
		printSide(s.Trainer, s)
	}
	switch {
	case b.Status == api.BattleStatusFinished:
		if w, ok := b.Winner.Get(); ok {
			fmt.Printf("\n%s won.\n", w)
		}
	default:
		if t, ok := b.Turn.Get(); ok && t != "" {
			fmt.Printf("\nwaiting on %s.\n", t)
		}
	}
}

func printSide(label string, s api.Side) {
	fmt.Printf("\n%s\n", label)
	for i, p := range s.Team {
		name := p.Name
		if p.Fainted {
			name += " (fainted)"
		}
		fmt.Printf("  %d %-22s %s %3d/%-3d %s\n",
			i+1, name, hpBar(p.Hp, p.MaxHp), p.Hp, p.MaxHp, strings.Join(p.Types, "/"))

		var notes []string
		if st := battletext.StageLabel(p); st != "" {
			notes = append(notes, st)
		}
		notes = append(notes, battletext.Conditions(p)...)
		if len(notes) > 0 {
			fmt.Printf("    %s\n", strings.Join(notes, " · "))
		}
	}
}

func hpBar(hp, max int) string {
	const width = 12
	if max <= 0 {
		return strings.Repeat("-", width)
	}
	filled := hp * width / max
	if filled < 0 {
		filled = 0
	}
	if hp > 0 && filled == 0 {
		filled = 1
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

func teamLine(s api.Side) string {
	names := make([]string, 0, len(s.Team))
	for _, p := range s.Team {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// identityHint prints what to do about an identity error, and clears
// a token the server has forgotten.
//
// The wording comes from battleclient.IdentityAdvice so the CLI, the
// TUI and the phone say the same thing. This used to be a second copy
// of the same errors.Is ladder with its own text, and the two had
// already drifted - the copy here still told players "trainers live
// in its memory", which stopped being true when trainers moved to
// postgres and a restart stopped losing them.
//
// The one thing that is genuinely the CLI's: clearing the stored
// token. That is recovery rather than wording, and it belongs where
// the file is.
//
// No return value: it only ever handed back its own argument, and
// main.go printed the error separately anyway.
func identityHint(err error) {
	advice := battleclient.IdentityAdvice(err)
	if advice == "" {
		return
	}
	fmt.Fprintln(os.Stderr, advice)

	if errors.Is(err, battleclient.ErrStaleIdentity) {
		if clearErr := battleclient.ClearIdentity(); clearErr != nil {
			fmt.Fprintf(os.Stderr, "  (could not remove the stored token: %v)\n", clearErr)
		}
	}
	fmt.Fprintln(os.Stderr, "  pokedex-cli register <your-name>")
}
