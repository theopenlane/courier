package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/theopenlane/courier/pkg/engine"
)

// appName is the application name used in log lines
const appName = "courier"

// flagConfig is the path to an explicit config file
var flagConfig string

// rootCmd is the base courier command
var rootCmd = &cobra.Command{
	Use:   "courier",
	Short: "export and import Openlane organization controls and policies as structured files",
	Long: `courier keeps a git repository of organization controls and internal
policies in sync with Openlane.

pull exports controls.yaml, policies.yaml, and policy markdown documents,
fmt normalizes the yaml files, and apply creates and updates records through
the API, writing only what differs and never deleting anything. Run apply with
--dry-run to see what it would change first.

Settings merge from a config file (default config/.config.yaml), COURIER_-prefixed
environment variables, and flags, later sources win.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		setupLogging(cmd.Flags())
	},
}

// setupLogging configures the logger based on the command flags
func setupLogging(flags *pflag.FlagSet) {
	log.Logger = zerolog.New(os.Stderr).
		With().Timestamp().
		Logger().
		With().Str("app", appName).
		Logger()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	if debug, _ := flags.GetBool("debug"); debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	if pretty, _ := flags.GetBool("pretty"); pretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
}

// Execute runs the root command and exits non-zero on error
func Execute() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run executes the root command under a context cancelled on interrupt, so a
// long pull or apply stops between requests instead of being killed mid-write
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return rootCmd.ExecuteContext(ctx)
}

// init registers the persistent flags
func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to a config file (default "+engine.DefaultConfigFile+")")
	rootCmd.PersistentFlags().String("host", engine.DefaultHost, "Openlane API host")
	rootCmd.PersistentFlags().String("token", "", "Openlane API token")
	rootCmd.PersistentFlags().String("organization-id", "", "organization ID header for multi-organization tokens")
	rootCmd.PersistentFlags().String("dir", engine.DefaultDir, "directory holding the exported files")
	rootCmd.PersistentFlags().Bool("debug", false, "enable debug logging")
	rootCmd.PersistentFlags().Bool("pretty", false, "enable pretty (human readable) logging output")
}

// loadSettings merges the config file, environment, and flags
func loadSettings() (engine.Settings, error) {
	return engine.LoadSettings(flagConfig, rootCmd.PersistentFlags())
}

// registerKindFlags adds one boolean flag per syncable kind to a command,
// mirroring the per-type seed flags in harmonize
func registerKindFlags(flags *pflag.FlagSet) {
	for _, kind := range engine.AllKinds() {
		flags.Bool(string(kind), false, "limit the operation to "+string(kind))
	}
}

// selectedKinds resolves the per-kind flags on a command into the kinds to
// operate on, none selected means all kinds
func selectedKinds(flags *pflag.FlagSet) []engine.Kind {
	selected := map[engine.Kind]bool{}

	for _, kind := range engine.AllKinds() {
		v, err := flags.GetBool(string(kind))
		if err != nil {
			continue
		}

		selected[kind] = v
	}

	return engine.SelectKinds(selected)
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
