// pokedex-cli
package main

// THIS WHOLE REPO IS YOURS. A CLI has no config.yaml, so `regen`,
// `render`, `check` and `vault` do not apply to it - `init` scaffolded
// these files once and nothing rewrites any of them. Edit freely.
//
// Two couplings survive that, and neither is checked at build time:
//
//   - VERSION drives releases. CI publishes only when its first line
//     changes on main, tagging what it says. Bump it in the commit that
//     should ship; a change without one builds and releases nothing.
//   - update.go downloads `pokedex-cli_<os>_<arch>.tar.gz` from the
//     latest GitHub release. CI builds assets with exactly that name.
//     Rename either side and self-update fails against a release that
//     looks fine on GitHub - so change both together, or neither.
//
// Because nothing regenerates this, a later convention change in
// homelabctl will not reach it. That is the trade for the repo being
// yours: `init` is a starting point, not a template you stay attached
// to.

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags; "dev" for local builds, which
// deliberately refuse to self-update.
var Version = "dev"

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pokedex-cli",
		Short: "pokedex-cli",
		// Usage on every error buries the error itself; cobra still prints
		// usage for genuine usage mistakes.
		SilenceUsage: true,
		// main prints the error, so cobra must not print it too. Without
		// this every failure appeared twice - once as "Error:" from
		// cobra and once as "error:" from main - which reads as two
		// things going wrong.
		SilenceErrors: true,
	}
	root.AddCommand(updateCmd(), versionCmd(), pokemonCmd(), showCmd(), typesCmd())
	// Battle mode: register once, then open or join, then attack per
	// turn. A CLI exits between commands, so the trainer token is stored
	// and `watch` is what makes a turn-based game playable this way.
	root.AddCommand(registerCmd(), lobbyCmd(), openCmd(), joinCmd(),
		showBattleCmd(), watchCmd(), attackCmd())

	// Cobra builds its own `completion` command during Execute, so it does
	// not exist yet here and cannot be extended in place. Force it to be
	// created now, then hang `install` off it.
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.AddCommand(completionInstallCmd("pokedex-cli"))
		}
	}
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(Version)
		},
	}
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		// Here rather than at the one place that loads the token: a
		// stale token is reported by the SERVER, so it surfaces from
		// whichever API call a command happened to make. Wrapping the
		// loader caught only the "never registered" half and left the
		// half that needs the same advice printing a bare 401.
		identityHint(err)
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// makeTeamCompleter completes Pokemon names at every team position,
// skipping the first `fixed` arguments (the battle id, for join).
//
// Separate from makeCompleter because that one deliberately stops after
// one argument - `show pikachu` takes exactly one Pokemon, and offering
// a second is wrong. A team takes three, so stopping at the first is
// what left `open pikachu <TAB>` silent and sent the player to look the
// next two names up somewhere else.
//
// Already-picked names are filtered out: the server rejects a duplicate
// team, and offering one is offering a mistake.
func makeTeamCompleter(list func() ([]string, error), fixed, max int) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) < fixed || len(args)-fixed >= max {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, err := list()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		picked := make(map[string]bool, len(args))
		for _, a := range args[fixed:] {
			picked[strings.ToLower(strings.TrimSpace(a))] = true
		}
		out := names[:0:0]
		for _, n := range names {
			if !picked[strings.ToLower(n)] {
				out = append(out, n)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// makeCompleter turns a function that lists names into a cobra completion
// function. Completions run on every TAB, so keep the lookup fast and fail
// quietly - returning ShellCompDirectiveError rather than nothing stops the
// shell falling back to offering filenames, which looks like a bug.
//
//	cmd.ValidArgsFunction = makeCompleter(listThings)
func makeCompleter(list func() ([]string, error)) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, err := list()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
