package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// battleclient.DefaultAPI, not a fourth copy of the hostname.
//
// The literal lived here, in battleclient, in the TUI and in the
// mobile app - four files across three modules, and moving the
// deployment meant finding all four with nothing failing if you
// missed one. The CLI already imports battleclient, so it can simply
// ask.
func baseURL() string {
	if u := strings.TrimSpace(os.Getenv("POKEDEX_URL")); u != "" {
		return u
	}
	return battleclient.DefaultAPI
}

func client() (*api.Client, error) {
	return api.NewClient(baseURL())
}

func withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// pokemonCmd lists the Pokedex, optionally filtered by type.
func pokemonCmd() *cobra.Command {
	var typ string
	var limit int

	cmd := &cobra.Command{
		Use:   "pokemon",
		Short: "list pokemon, optionally filtered by type",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			params := api.ListPokemonParams{}
			if typ != "" {
				params.Type = api.NewOptString(typ)
			}
			if limit > 0 {
				params.Limit = api.NewOptInt(limit)
			}
			list, err := c.ListPokemon(ctx, params)
			if err != nil {
				return fmt.Errorf("listing pokemon: %w", err)
			}
			if list.Count == 0 {
				fmt.Printf("no pokemon match type %q\n", typ)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "#\tNAME\tTYPES")
			for _, p := range list.Pokemon {
				fmt.Fprintf(w, "%d\t%s\t%s\n", p.ID, p.Name, strings.Join(p.Types, ", "))
			}
			w.Flush()
			fmt.Printf("\n%d pokemon\n", list.Count)
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "only pokemon of this type, e.g. fire")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many (0 = all)")
	_ = cmd.RegisterFlagCompletionFunc("type", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		names, err := typeNames()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// showCmd prints one Pokemon as a card.
func showCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "show one pokemon in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			res, err := c.GetPokemon(ctx, api.GetPokemonParams{Name: strings.ToLower(args[0])})
			if err != nil {
				return fmt.Errorf("fetching %s: %w", args[0], err)
			}
			switch v := res.(type) {
			case *api.Pokemon:
				printCard(*v)
				return nil
			case *api.Error:
				return fmt.Errorf("%s", v.Message)
			default:
				return fmt.Errorf("unexpected response %T", res)
			}
		},
	}
	cmd.ValidArgsFunction = makeCompleter(pokemonNames)
	return cmd
}

// typesCmd lists every type with a count.
func typesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types",
		Short: "list every type in the pokedex",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			ctx, cancel := withTimeout()
			defer cancel()

			list, err := c.ListTypes(ctx)
			if err != nil {
				return fmt.Errorf("listing types: %w", err)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TYPE\tPOKEMON")
			for _, t := range list.Types {
				fmt.Fprintf(w, "%s\t%d\n", t.Name, t.Count)
			}
			return w.Flush()
		},
	}
}

func printCard(p api.Pokemon) {
	fmt.Printf("\n  #%03d  %s\n", p.ID, strings.ToUpper(p.Name))
	fmt.Printf("  %s\n\n", strings.Join(p.Types, " / "))
	fmt.Printf("  height  %.1f m\n", float64(p.Height)/10)
	fmt.Printf("  weight  %.1f kg\n", float64(p.Weight)/10)
	fmt.Printf("  sprite  %s\n", p.Sprite)

	if len(p.Moves) == 0 {
		fmt.Println()
		return
	}
	fmt.Println("\n  moves")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, m := range p.Moves {
		power := fmt.Sprintf("%d", m.Power)
		if m.Power == 0 {
			// A status move deals no damage; printing 0 reads as a bug.
			power = "-"
		}
		fmt.Fprintf(w, "  \t%s\t%s\t%s\n", m.Name, m.Type, power)
	}
	w.Flush()
	fmt.Println()
}

// pokemonNames backs tab-completion for `show`.
func pokemonNames() ([]string, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	list, err := c.ListPokemon(ctx, api.ListPokemonParams{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Pokemon))
	for _, p := range list.Pokemon {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names, nil
}

func typeNames() ([]string, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	list, err := c.ListTypes(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Types))
	for _, t := range list.Types {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names, nil
}
