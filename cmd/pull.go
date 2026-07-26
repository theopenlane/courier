package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "export organization controls, mappings, and policies into the workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, settings, err := newClient()
		if err != nil {
			return err
		}

		result, err := client.Pull(cmd.Context(), settings.Dir)
		if err != nil {
			return err
		}

		fmt.Printf("exported %d controls and %d policies\n", result.TotalControls, result.TotalPolicies)
		printList("written", result.Written)
		printList("removed", result.Removed)

		if len(result.Written)+len(result.Removed) == 0 {
			fmt.Println("workspace already up to date")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullCmd)
}
