package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

// flagApplyFile applies a single yaml file resolved by content
var flagApplyFile string

// applyCmd pushes the store files to Openlane
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "push store controls, mappings, and policies to Openlane",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, settings, err := newClient()
		if err != nil {
			return err
		}

		var (
			store *engine.Store
			kinds []engine.Kind
		)

		if flagApplyFile != "" {
			store, kinds, err = engine.LoadFile(flagApplyFile)
		} else {
			kinds = selectedKinds(cmd.Flags())
			store, err = engine.NewStore(settings.Dir)
		}

		if err != nil {
			return err
		}

		result, err := client.Apply(cmd.Context(), store, kinds)
		if err != nil {
			return err
		}

		fmt.Printf(
			"applied: %d controls created, %d controls updated, %d mappings created, %d policies created, %d policies updated\n",
			len(result.CreatedControls), len(result.UpdatedControls), result.CreatedMappings,
			len(result.CreatedPolicies), len(result.UpdatedPolicies),
		)

		printList("warnings", result.Warnings)
		printList("errors", result.Errors)

		if len(result.CreatedControls)+len(result.CreatedPolicies) > 0 {
			fmt.Println("run 'courier pull' to write new IDs back to the store")
		}

		if len(result.Errors) > 0 {
			return fmt.Errorf("%w: %d records failed", ErrApplyIncomplete, len(result.Errors))
		}

		return nil
	},
}

// init registers the apply command
func init() {
	registerKindFlags(applyCmd.Flags())
	applyCmd.Flags().StringVarP(&flagApplyFile, "file", "f", "", "path to a yaml file to apply, resolved to controls or policies by content")
	rootCmd.AddCommand(applyCmd)
}
