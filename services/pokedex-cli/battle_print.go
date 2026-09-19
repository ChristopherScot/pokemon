package main

// Turning battle state into terminal output.
//
// Separate from battle.go, which is cobra command wiring, because the
// two change for different reasons: a new flag is a command change, a
// clearer board is a printing change. The seam was already there -
// every command above delegates to these - so this is where the file
// was going to split anyway.
//
// Printing to stdout directly rather than through an io.Writer: nothing
// tests this output today, and the idiomatic way to make it testable
// when something does is to thread cmd.OutOrStdout() through, which
// cobra already provides. An interface here would be machinery without
// a caller.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

// index1 converts a 1-based argument to the 0-based index the API wants,
// rejecting anything that is not a position.
func index1(s, what string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", what, s)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: positions start at 1", what)
	}
	return n - 1, nil
}

// printEvent writes one log line with a glyph for what it did.
//
// The CLI cannot animate a hit the way the web does, so the glyph is
// how a super-effective hit reads differently from an ordinary one at a
// glance. It leads the line and is padded to a fixed width, because a
// column that moves with the content is harder to scan than no column.
//
// Events with no glyph still get the indent, so the prose stays aligned.
func printEvent(ev api.BattleEvent) {
	if icon := battletext.EventIcon(ev); icon != "" {
		fmt.Printf(" %s %s\n", icon, ev.Text)
		return
	}
	fmt.Println("   ", ev.Text)
}

// lastTurn is the events from the most recent turn, which is what a
// player wants to see after attacking.
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

// printBattle draws the board: both teams with HP bars, and whose move
// it is. 1-based positions, matching what attack expects.
func printBattle(c *battleclient.Client, b *api.Battle) {
	mine, theirs, ok := c.SideFor(b)
	if !ok {
		fmt.Printf("battle %s: waiting for an opponent\n", b.ID)
		if len(b.Sides) == 1 {
			fmt.Printf("  %s: %s\n", b.Sides[0].Trainer, teamLine(b.Sides[0]))
		}
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
		// The moves, with their numbers. Picking "move 4" out of a
		// board that never lists them means going to `show <name>` for
		// every turn, which is not a CLI anyone wants to use.
		fmt.Println("\nyour moves")
		for i, p := range mine.Team {
			if p.Fainted {
				continue
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
		fmt.Printf("\nyour move: pokedex-cli attack %s <your 1-3> <move 1-6> <their 1-3>\n", b.ID)
	default:
		fmt.Printf("\nwaiting on %s. `watch %s` to block until it is your turn.\n", b.Turn.Value, b.ID)
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

		// Stat changes and conditions on their own line, indented under
		// the Pokemon they belong to. Only when there is something to
		// say - a battle where nothing has been buffed prints nothing
		// extra.
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

// hpBar is plain ASCII: this prints into pipes and logs as often as a
// terminal, and a block-drawing bar there is noise.
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
		// A Pokemon that is alive should never show an empty bar; that
		// reads as fainted.
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

// identityHint turns an identity error into the command that fixes it,
// rather than making someone read help to find out.
//
// A stale token also gets DELETED, not just explained. Trainers live in
// the server's memory, so every deploy invalidates every stored token;
// keeping one means the same failure on every command with nothing
// saying the file is the problem. Removing it makes the next run take
// the ordinary unregistered path.
func identityHint(err error) error {
	switch {
	case errors.Is(err, battleclient.ErrNoIdentity):
		fmt.Fprintln(os.Stderr, "no trainer registered yet.")
		fmt.Fprintln(os.Stderr, "  pokedex-cli register <your-name>")
	case errors.Is(err, battleclient.ErrStaleIdentity):
		fmt.Fprintln(os.Stderr, "the server no longer knows this trainer -")
		fmt.Fprintln(os.Stderr, "it restarted, and trainers live in its memory.")
		if clearErr := battleclient.ClearIdentity(); clearErr != nil {
			fmt.Fprintf(os.Stderr, "  (could not remove the stored token: %v)\n", clearErr)
		}
		fmt.Fprintln(os.Stderr, "  pokedex-cli register <your-name>")
	}
	return err
}
