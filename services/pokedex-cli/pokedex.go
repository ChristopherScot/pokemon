package main

// The `pokemon`, `types` and `show` commands, backed by the pokedex
// service's GENERATED client.
//
// Nothing here hand-writes a URL, a query string or a JSON struct: api/
// is committed in the service repo, so this imports it the way any Go
// consumer would. When the spec changes, regenerating the service and
// bumping this dependency makes the compiler point at whatever no longer
// fits - which is the whole reason the client is generated rather than
// written twice.

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
)

// defaultBaseURL is the DEPLOYED Pokedex, reachable from anywhere.
// $POKEDEX_URL overrides it, so the same binary works against a local
// server or a port-forward without a rebuild.
//
// Not the in-cluster address, which is what this used to be: this
// binary is published as a release and run on laptops, where
// pokedex.pokedex.svc.cluster.local does not resolve and the failure -
// "no such host" - reads as a broken install rather than a default
// that only works in one place. The TUI already pointed here; the two
// disagreeing was the bug.
const defaultBaseURL = "https://pokemon.home.chrisscotmartin.com/api"

func baseURL() string {
	if u := strings.TrimSpace(os.Getenv("POKEDEX_URL")); u != "" {
		return u
	}
	return defaultBaseURL
}

// client builds the generated client. The timeout is short on purpose:
// this is an interactive CLI, and a request that has not answered in a
// few seconds should say so rather than appear to hang.
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
			// The spec declares a 404, so the generated client returns it
			// as a VALUE rather than an error - an unknown name is an
			// answer, and this switch is the compiler making us handle it.
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

// printCard renders a Pokemon the way the web UI renders a card, so the
// two consumers show the same thing in their own idiom.
func printCard(p api.Pokemon) {
	fmt.Printf("\n  #%03d  %s\n", p.ID, strings.ToUpper(p.Name))
	fmt.Printf("  %s\n\n", strings.Join(p.Types, " / "))
	// Decimetres and hectograms are what the Pokedex reports; converting
	// here keeps the API honest to its source and the CLI readable.
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
