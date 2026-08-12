package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

var (
	// flagApplyFile applies a single yaml file resolved by content
	flagApplyFile string
	// flagDryRun reports what apply would change without writing anything
	flagDryRun bool
	// flagKeepStatus writes the status the files carry rather than sending
	// edited records back for approval
	flagKeepStatus bool
	// flagReport renders the run as a write-up to share, and never writes to
	// Openlane
	flagReport bool
	// flagOutput writes the report to a file as markdown
	flagOutput string
)

// applyCmd pushes the store files to Openlane
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "push store controls, mappings, and policies to Openlane",
	Long: `apply pushes the store files to Openlane, creating records that do not
exist and updating the ones whose managed fields differ. Records that already
match are left alone, so a repeated apply is a no-op and nothing is deleted.

--dry-run runs the same comparison and reports what would change without
writing anything.

Editing a control's description, or a policy's body, moves that record to
NEEDS_APPROVAL so changed language is not left standing as approved.
--keep-status turns that off and writes the status the files carry.

--report renders the run as a write-up to share, listing the records created and
the value before and after for every field an update changed, rather than the
one-line-per-record output of a run. It implies --dry-run, a report is something
you read before deciding, so it never writes to Openlane. Pass -o to write it to
a file as markdown instead of printing it.`,
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

		result, err := client.Apply(cmd.Context(), store, kinds,
			engine.ApplyOptions{DryRun: flagDryRun || flagReport, KeepStatus: flagKeepStatus})
		if err != nil {
			return err
		}

		// the controls Openlane holds that the files no longer do, reported
		// alongside the run rather than acted on, courier never deletes. Only
		// when the whole store was applied: a single file is not the inventory
		// to compare against, everything outside it would read as removed
		var archivable *engine.ArchivableResult

		if reporting() && flagApplyFile == "" && containsKind(kinds, engine.KindControls) {
			if archivable, err = client.Archivable(cmd.Context(), store, true); err != nil {
				return err
			}
		}

		// rendered before the error return, a run that failed part way is the
		// one the write-up is most worth having
		if err := renderApply(result, archivable); err != nil {
			return err
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
	applyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "report what apply would change without writing anything")
	applyCmd.Flags().BoolVar(&flagKeepStatus, "keep-status", false,
		"write the status the files carry, rather than sending records with edited language back for approval")
	applyCmd.Flags().BoolVar(&flagReport, "report", false,
		"render the run as a write-up of what would change, rather than an operator's view, and write nothing to Openlane")
	applyCmd.Flags().StringVarP(&flagOutput, "output", "o", "",
		"write the report to this path as markdown, replacing what is there, rather than printing it")

	// --file resolves its own kind from the file content, so a kind flag alongside it is ignored
	for _, kind := range engine.AllKinds() {
		applyCmd.MarkFlagsMutuallyExclusive("file", string(kind))
	}

	rootCmd.AddCommand(applyCmd)
}

// reporting reports whether the run renders as a report, a path to write is
// one whether or not --report was passed, there is nothing else a file of
// results would hold
func reporting() bool {
	return flagReport || flagOutput != ""
}

// renderApply prints what the run changed, or would change on a dry run, as
// the operator's view or as the report
func renderApply(result *engine.ApplyResult, archivable *engine.ArchivableResult) error {
	if !reporting() {
		renderApplyText(result)

		return nil
	}

	run := runReport{result: result, archivable: archivable, dryRun: flagDryRun || flagReport, at: time.Now()}

	if flagOutput == "" {
		fmt.Print(renderSummary(run))

		return nil
	}

	if err := writeReport(flagOutput, renderReport(run)); err != nil {
		return err
	}

	fmt.Printf("report written to %s\n", flagOutput)

	return nil
}

// renderApplyText prints one line per record, the operator's view of a run
func renderApplyText(result *engine.ApplyResult) {
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
