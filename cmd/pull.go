package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pullCmd exports Openlane records into the store
var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "export organization controls, mappings, and policies into the store",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, settings, err := newClient()
		if err != nil {
			return err
		}

		result, err := client.Pull(cmd.Context(), settings.Dir, selectedKinds(cmd.Flags()))
		if err != nil {
			return err
		}

		fmt.Printf("exported %d controls and %d policies\n", result.TotalControls, result.TotalPolicies)
		printList("written", result.Written)
		printList("removed", result.Removed)
		printList("warnings", result.Warnings)

		if len(result.Written)+len(result.Removed) == 0 {
			fmt.Println("store already up to date")
		}

		return nil
	},
}

// init registers the pull command
func init() {
	registerKindFlags(pullCmd.Flags())
	rootCmd.AddCommand(pullCmd)
}
