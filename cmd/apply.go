package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

var (
	// flagApplyFile applies a single yaml file resolved by content
	flagApplyFile string
	// flagDryRun reports what apply would change without writing anything
	flagDryRun bool
)

// applyCmd pushes the store files to Openlane
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "push store controls, mappings, and policies to Openlane",
	Long: `apply pushes the store files to Openlane, creating records that do not
exist and updating the ones whose managed fields differ. Records that already
match are left alone, so a repeated apply is a no-op and nothing is deleted.

--dry-run runs the same comparison and reports what would change without
writing anything.`,
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

		result, err := client.Apply(cmd.Context(), store, kinds, engine.ApplyOptions{DryRun: flagDryRun})
		if err != nil {
			return err
		}

		renderApply(result)

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
	applyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "report what apply would change without writing anything")

	// --file resolves its own kind from the file content, so a kind flag alongside it is ignored
	for _, kind := range engine.AllKinds() {
		applyCmd.MarkFlagsMutuallyExclusive("file", string(kind))
	}

	rootCmd.AddCommand(applyCmd)
}

// renderApply prints what the run changed, or would change on a dry run
func renderApply(result *engine.ApplyResult) {
	verb := "was"
	if flagDryRun {
		verb = "will be"
	}

	printChanges("+", "control", verb+" created", result.CreatedControls)
	printChanges("~", "control", verb+" updated", result.UpdatedControls)
	printChanges("+", "mapping", verb+" created", result.CreatedMappings)
	printChanges("~", "mapping", verb+" extended", result.UpdatedMappings)
	printChanges("+", "policy", verb+" created", result.CreatedPolicies)
	printChanges("~", "policy", verb+" updated", result.UpdatedPolicies)

	for _, warning := range result.Warnings {
		fmt.Printf("? %s\n", warning)
	}

	if unchanged := result.UnchangedControls + result.UnchangedPolicies; unchanged > 0 {
		fmt.Printf("= %d records already match Openlane\n", unchanged)
	}

	changed := len(result.CreatedControls) + len(result.UpdatedControls) +
		len(result.CreatedMappings) + len(result.UpdatedMappings) +
		len(result.CreatedPolicies) + len(result.UpdatedPolicies)

	if changed == 0 && len(result.Errors) == 0 {
		fmt.Println("nothing to do, the files match Openlane")
	}

	printList("errors", result.Errors)

	if !flagDryRun && len(result.CreatedControls)+len(result.CreatedPolicies) > 0 {
		fmt.Println("run 'courier pull' to write new IDs back to the store")
	}
}

// printChanges prints one line per change, naming the fields or targets that differ
func printChanges(marker, kind, action string, changes []engine.Change) {
	for _, change := range changes {
		if len(change.Detail) == 0 {
			fmt.Printf("%s %s %s %s\n", marker, kind, change.Ref, action)

			continue
		}

		fmt.Printf("%s %s %s %s (%s)\n", marker, kind, change.Ref, action, strings.Join(change.Detail, ", "))
	}
}
