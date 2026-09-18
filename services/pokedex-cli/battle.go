package main

// Battle commands. The rules live in the API and the plumbing lives in
// battleclient; what is here is argument parsing and printing.
//
// A CLI exits between commands, so a battle is played as a sequence of
// invocations against stored identity - `register` once, then `open` or
// `join`, then `attack` per turn. `watch` blocks until it is your move,
// which is what makes that sequence bearable.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// battleClient loads the stored trainer and points it at the same API
// the rest of the CLI uses.
func battleClient() (*battleclient.Client, error) {
	id, err := battleclient.LoadIdentity()
	if err != nil {
		// Every battle command goes through here, so the "register
		// first" hint is attached once rather than at seven call sites.
		return nil, identityHint(err)
	}
	// The env var wins over whatever was stored, so a port-forward works
	// without re-registering.
	id.API = baseURL()
	return battleclient.New(id)
}

func registerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register <trainer-name>",
		Short: "claim a trainer name so other trainers can find you",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := withTimeout()
			defer cancel()

			id, err := battleclient.Register(ctx, baseURL(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("registered as %s\n", id.Name)
			fmt.Println("you can now `open` a battle or `join` someone else's")
			return nil
		},
	}
}

func lobbyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lobby",
		Short: "trainers waiting for an opponent",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			list, err := c.Lobby(ctx)
			if err != nil {
				return err
			}
			if list.Count == 0 {
				fmt.Println("nobody is waiting. `open` a battle and someone can join you")
				return nil
			}
			for _, w := range list.Waiting {
				fmt.Printf("%-10s %-8s %s\n", w.BattleId, w.Trainer, strings.Join(w.Team, ", "))
			}
			fmt.Printf("\njoin one with: pokedex-cli join <id> <three pokemon>\n")
			return nil
		},
	}
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [pokemon pokemon pokemon]",
		Short: "open a battle and wait for an opponent",
		Long: "Names three Pokemon, or none at all for a random team - " +
			"which is the fastest way to get into a battle.",
		Args: cobra.RangeArgs(0, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			if n := len(args); n != 0 && n != 3 {
				return fmt.Errorf("name three pokemon, or none for a random team")
			}
			c, err := battleClient()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			b, err := c.Create(ctx, args)
			if err != nil {
				return err
			}
			fmt.Printf("battle %s is open. tell your opponent:\n", b.ID)
			fmt.Printf("  pokedex-cli join %s <three pokemon>\n\n", b.ID)
			fmt.Printf("then watch for your turn:\n  pokedex-cli watch %s\n", b.ID)
			return nil
		},
	}
}

func joinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "join <battle-id> [pokemon pokemon pokemon]",
		Short: "join a waiting battle",
		Long:  "Names three Pokemon, or none at all for a random team.",
		Args:  cobra.RangeArgs(1, 4),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			if n := len(args) - 1; n != 0 && n != 3 {
				return fmt.Errorf("name three pokemon, or none for a random team")
			}
			b, err := c.Join(ctx, args[0], args[1:])
			if err != nil {
				return err
			}
			printBattle(c, b)
			return nil
		},
	}
}

func showBattleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "battle <battle-id>",
		Short: "show the state of a battle",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			b, err := c.Get(ctx, args[0])
			if err != nil {
				return err
			}
			printBattle(c, b)
			return nil
		},
	}
}

func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch <battle-id>",
		Short: "block until it is your turn, printing what happens",
		Long: "Polls the battle and prints each new event as it lands, " +
			"returning when it is your move or the battle ends.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			// No withTimeout here: waiting for a human opponent can take
			// minutes, and a ten-second deadline would make watch
			// useless for the one job it has.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			b, err := c.Get(ctx, args[0])
			if err != nil {
				return err
			}
			printBattle(c, b)

			seen := b.Version
			shown := len(b.Log)
			for b.Status != api.BattleStatusFinished && !c.MyTurn(b) {
				b, err = c.Watch(ctx, args[0], seen)
				if err != nil {
					return err
				}
				// Only what is new: re-printing the whole log every poll
				// would bury the turn that just happened.
				for _, ev := range b.Log[min(shown, len(b.Log)):] {
					fmt.Println(" ", ev.Text)
				}
				shown = len(b.Log)
				seen = b.Version
			}

			fmt.Println()
			printBattle(c, b)
			return nil
		},
	}
}

func attackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attack <battle-id> <my-pokemon> <move> <their-pokemon>",
		Short: "attack, naming your pokemon, its move and the target",
		Long: "Positions are 1-based, as `battle` prints them: " +
			"`attack abc123 1 2 3` is your first Pokemon using its second " +
			"move on their third.",
		Args: cobra.ExactArgs(4),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			// 1-based on the command line because that is how the board
			// prints; the API is 0-based.
			attacker, err := index1(args[1], "your pokemon")
			if err != nil {
				return err
			}
			move, err := index1(args[2], "move")
			if err != nil {
				return err
			}
			target, err := index1(args[3], "their pokemon")
			if err != nil {
				return err
			}

			ctx, cancel := withTimeout()
			defer cancel()
			b, err := c.Attack(ctx, args[0], attacker, move, target)
			if err != nil {
				return err
			}
			for _, ev := range lastTurn(b) {
				fmt.Println(" ", ev.Text)
			}
			fmt.Println()
			printBattle(c, b)
			return nil
		},
	}
}

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

// identityHint turns the "no trainer" error into the command that fixes
// it, rather than making someone read help to find out.
func identityHint(err error) error {
	if errors.Is(err, battleclient.ErrNoIdentity) {
		fmt.Fprintln(os.Stderr, "no trainer registered yet.")
		fmt.Fprintln(os.Stderr, "  pokedex-cli register <your-name>")
	}
	return err
}
