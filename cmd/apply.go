package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// applyCmd creates and updates Openlane records from the workspace
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "create and update Openlane controls, mappings, and policies from the workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, plan, err := computePlan(cmd.Context())
		if err != nil {
			return err
		}

		if !plan.HasChanges() {
			fmt.Println("no changes, Openlane matches the workspace")

			return nil
		}

		renderPlan(plan)

		result, err := client.Apply(cmd.Context(), plan)
		if err != nil {
			return err
		}

		fmt.Printf(
			"applied: %d controls created, %d controls updated, %d mappings created, %d policies created, %d policies updated\n",
			len(result.CreatedControls), len(result.UpdatedControls), result.CreatedMappings,
			len(result.CreatedPolicies), len(result.UpdatedPolicies),
		)

		printList("warnings", result.Warnings)

		if len(result.CreatedControls)+len(result.CreatedPolicies) > 0 {
			fmt.Println("run 'courier pull' on a branch to write new IDs back to the workspace")
		}

		return nil
	},
}

// init registers the apply command
func init() {
	rootCmd.AddCommand(applyCmd)
}
