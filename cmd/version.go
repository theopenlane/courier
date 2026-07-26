package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/constants"
)

var flagShortVersion bool

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

func init() {
	versionCmd.Flags().BoolVar(&flagShortVersion, "short", false, "print only the version number")
	rootCmd.AddCommand(versionCmd)
}
