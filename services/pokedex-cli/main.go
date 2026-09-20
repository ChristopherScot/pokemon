// pokedex-cli
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var Version = "dev"

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "pokedex-cli",
		Short:         "pokedex-cli",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(updateCmd(), versionCmd(), pokemonCmd(), showCmd(), typesCmd())
	root.AddCommand(registerCmd(), lobbyCmd(), openCmd(), joinCmd(),
		showBattleCmd(), watchCmd(), attackCmd())

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
		identityHint(err)
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

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
