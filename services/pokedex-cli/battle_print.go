package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

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
		fmt.Printf("\nyour move: pokedex-cli attack %s <your 1-%d> <move 1-%d> <their 1-%d>\n",
			b.ID, len(mine.Team), len(mine.Team[0].Moves), len(theirs.Team))
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
