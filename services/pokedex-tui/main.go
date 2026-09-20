// pokedex-tui
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

const defaultAPI = "https://pokemon.home.chrisscotmartin.com/api"

var Version = "dev"

func rootCmd() *cobra.Command {
	var apiURL string

	root := &cobra.Command{
		Use:          "pokedex-tui",
		Short:        "pokedex-tui",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(apiURL)
		},
	}

	def := defaultAPI
	if v := os.Getenv("POKEDEX_URL"); v != "" {
		def = v
	}
	root.Flags().StringVar(&apiURL, "api", def, "base URL of the Pokedex API")
	root.AddCommand(updateCmd(), versionCmd())

	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.AddCommand(completionInstallCmd("pokedex-tui"))
		}
	}
	return root
}

func run(apiURL string) error {
	c, err := api.NewClient(apiURL)
	if err != nil {
		return fmt.Errorf("pokedex api at %s: %w", apiURL, err)
	}

	_, err = tea.NewProgram(newModel(c, apiURL)).Run()
	return err
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
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
