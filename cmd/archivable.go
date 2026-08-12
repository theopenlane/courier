package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

var (
	// flagArchivableSubcontrols reports archivable subcontrols as well
	flagArchivableSubcontrols bool
	// flagArchivableRefCodes prints refCodes alone, for piping
	flagArchivableRefCodes bool
)

// archivableCmd reports controls in Openlane the store no longer holds
var archivableCmd = &cobra.Command{
	Use:   "archivable [file]",
	Short: "list controls in Openlane that the store no longer holds",
	Long: `archivable compares your controls against Openlane and lists the ones that
exist there and are no longer in your files, the candidates to archive after
an inventory has been rewritten from a spreadsheet.

Only controls without a reference framework are listed: a control that derives
from a standard is not yours to retire, so it is never reported even when your
files do not carry it. Records match the way apply matches them, on the
Openlane ID your files carry and on refCode otherwise.

Nothing is archived, courier never deletes; this reports what to review in the
Openlane UI. Pass a file to compare that inventory instead of the store.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runArchivable,
}

// runArchivable loads the inventory to compare and reports what Openlane
// holds that it does not
func runArchivable(cmd *cobra.Command, args []string) error {
	client, settings, err := newClient()
	if err != nil {
		return err
	}

	store, err := archivableStore(settings.Dir, args)
	if err != nil {
		return err
	}

	result, err := client.Archivable(cmd.Context(), store, flagArchivableSubcontrols)
	if err != nil {
		return err
	}

	if flagArchivableRefCodes {
		for _, control := range result.Controls {
			fmt.Println(control.RefCode)
		}

		return nil
	}

	if len(result.Controls) == 0 {
		fmt.Printf("all %d controls in Openlane are still in your files\n", result.TotalRemote)

		return nil
	}

	fmt.Printf("archivable (%d of %d):\n", len(result.Controls), result.TotalRemote)

	for _, control := range result.Controls {
		fmt.Printf("  %s\n", archivableLine(control))
	}

	return nil
}

// archivableStore loads the inventory to compare, the store directory when no
// file is named
func archivableStore(dir string, args []string) (*engine.Store, error) {
	if len(args) == 0 {
		return engine.NewStore(dir)
	}

	store, kinds, err := engine.LoadFile(args[0])
	if err != nil {
		return nil, err
	}

	if !containsKind(kinds, engine.KindControls) {
		return nil, fmtCmdErr(ErrNotAControlInventory, args[0])
	}

	return store, nil
}

// archivableLine renders one archivable record
func archivableLine(control engine.ArchivableControl) string {
	line := control.RefCode
	if control.Subcontrol {
		line = control.ParentRefCode + "/" + control.RefCode
	}

	if control.Title != "" {
		line += "  " + control.Title
	}

	if control.Status != "" {
		line += "  [" + control.Status + "]"
	}

	return line
}

// containsKind reports whether a kind is in the list
func containsKind(kinds []engine.Kind, kind engine.Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}

	return false
}

// init registers the archivable command
func init() {
	archivableCmd.Flags().BoolVar(&flagArchivableSubcontrols, "subcontrols", false, "report archivable subcontrols as well as controls")
	archivableCmd.Flags().BoolVar(&flagArchivableRefCodes, "ref-codes", false, "print refCodes alone, one per line")

	rootCmd.AddCommand(archivableCmd)
}
