package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func battleClient() (*battleclient.Client, error) {
	id, err := battleclient.LoadIdentity()
	if err != nil {
		return nil, err
	}
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

const teamSize = battleclient.TeamSize

func openCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open [pokemon pokemon pokemon]",
		Short: "open a battle and wait for an opponent",
		Long: "Names up to three Pokemon. The server fills whatever you " +
			"leave out, so naming none at all is the fastest way into a " +
			"battle and naming one is a perfectly good way to start.",
		Args: cobra.RangeArgs(0, teamSize),
		RunE: func(_ *cobra.Command, args []string) error {
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

			turn := battleclient.Turn{Attacker: attacker, Move: move, Target: target}
			cur, err := c.Get(ctx, args[0])
			if err != nil {
				return err
			}
			if err := c.CheckTurn(cur, turn); err != nil {
				return err
			}

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
