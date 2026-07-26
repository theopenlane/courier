package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/constants"
)

// flagShortVersion prints only the version number
var flagShortVersion bool

// versionCmd prints the courier version
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print the courier version",
	Run: func(_ *cobra.Command, _ []string) {
		if flagShortVersion {
			fmt.Println(constants.CLIVersion)

			return
		}

		fmt.Println(constants.VerboseCLIVersion)
	},
}

// init registers the version command
func init() {
	versionCmd.Flags().BoolVar(&flagShortVersion, "short", false, "print only the version number")
	rootCmd.AddCommand(versionCmd)
}
