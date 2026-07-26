package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

// defaultHost is the Openlane API host used when none is configured
const defaultHost = "https://api.theopenlane.io"

// flagConfig is the path to an explicit config file
var flagConfig string

// rootCmd is the base courier command
var rootCmd = &cobra.Command{
	Use:   "courier",
	Short: "export and import Openlane organization controls and policies as structured files",
	Long: `courier keeps a git repository of organization controls and internal
policies in sync with Openlane.

pull exports controls.yaml, policies.yaml, and policy markdown documents,
fmt normalizes the yaml files, plan shows what apply would change, and apply
creates and updates records through the API, nothing is ever deleted.

Settings merge from a config file (default .courier.yaml), COURIER_-prefixed
environment variables, and flags, later sources win.`,
	SilenceUsage: true,
}

// Execute runs the root command and exits non-zero on error
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// init registers the persistent flags
func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to a config file (default "+engine.DefaultConfigFile+")")
	rootCmd.PersistentFlags().String("host", defaultHost, "Openlane API host")
	rootCmd.PersistentFlags().String("token", "", "Openlane API token")
	rootCmd.PersistentFlags().String("organization-id", "", "organization ID header for multi-organization tokens")
	rootCmd.PersistentFlags().String("dir", ".", "workspace directory holding the exported files")
}

// loadSettings merges the config file, environment, and flags
func loadSettings() (engine.Settings, error) {
	return engine.LoadSettings(flagConfig, rootCmd.PersistentFlags())
}

// newClient builds an API client from the merged settings
func newClient() (*engine.Client, engine.Settings, error) {
	settings, err := loadSettings()
	if err != nil {
		return nil, settings, err
	}

	client, err := engine.NewClient(engine.Config{
		Host:           settings.Host,
		Token:          settings.Token,
		OrganizationID: settings.OrganizationID,
	})

	return client, settings, err
}

// printList prints a labeled list of items when non-empty
func printList(label string, items []string) {
	if len(items) == 0 {
		return
	}

	fmt.Printf("%s (%d):\n", label, len(items))

	for _, item := range items {
		fmt.Printf("  %s\n", item)
	}
}
