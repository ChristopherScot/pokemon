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
	"fmt"
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
		return nil, err
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

// teamSize is how many Pokemon a battle side holds. Named so the
// argument limits and the TAB completer cannot disagree about it - they
// were two separate literals, and a completer that offers a fourth name
// the parser then rejects is worse than no completer.
const teamSize = 3

func openCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open [pokemon pokemon pokemon]",
		Short: "open a battle and wait for an opponent",
		Long: "Names up to three Pokemon. The server fills whatever you " +
			"leave out, so naming none at all is the fastest way into a " +
			"battle and naming one is a perfectly good way to start.",
		Args: cobra.RangeArgs(0, teamSize),
		RunE: func(_ *cobra.Command, args []string) error {
			// No "0 or exactly 3" check. The API took partial teams from
			// 0.4.0 and both other clients offer them; this rule only
			// lived here, so the CLI rejected a request the server would
			// have accepted.
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
			fmt.Printf("  pokedex-cli join %s [up to three pokemon]\n\n", b.ID)
			fmt.Printf("then watch for your turn:\n  pokedex-cli watch %s\n", b.ID)
			return nil
		},
	}
	// TAB completes every team slot, not just the first. Typing three
	// Pokemon from memory was the CLI's version of the bug the web had:
	// the names exist, the completer existed, and the two were never
	// connected on the commands that take a team.
	cmd.ValidArgsFunction = makeTeamCompleter(pokemonNames, 0, teamSize)
	return cmd
}

func joinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join <battle-id> [pokemon pokemon pokemon]",
		Short: "join a waiting battle",
		Long:  "Names up to three Pokemon; the server fills the rest.",
		Args:  cobra.RangeArgs(1, teamSize+1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := battleClient()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			b, err := c.Join(ctx, args[0], args[1:])
			if err != nil {
				return err
			}
			printBattle(c, b)
			return nil
		},
	}
	// fixed=1: argument one is the battle id, so the team starts at two.
	cmd.ValidArgsFunction = makeTeamCompleter(pokemonNames, 1, teamSize)
	return cmd
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
					printEvent(ev)
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
			"move on their third.\n\n" +
			"A move shown as (disabled) cannot be used this turn.",
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
				printEvent(ev)
			}
			fmt.Println()
			printBattle(c, b)
			return nil
		},
	}
}
