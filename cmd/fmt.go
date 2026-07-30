package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

// flagFmtCheck fails fmt instead of rewriting files when set
var flagFmtCheck bool

// fmtCmd normalizes the store yaml files into canonical form
var fmtCmd = &cobra.Command{
	Use:   "fmt",
	Short: "normalize controls.yaml and policies.yaml into canonical form",
	RunE: func(_ *cobra.Command, _ []string) error {
		settings, err := loadSettings()
		if err != nil {
			return err
		}

		result, err := engine.Format(settings.Dir, flagFmtCheck)
		if err != nil {
			return err
		}

		if len(result.Changed) == 0 {
			fmt.Println("all files are canonical")

			return nil
		}

		if flagFmtCheck {
			printList("not canonical", result.Changed)

			return ErrNotFormatted
		}

		printList("formatted", result.Changed)

		return nil
	},
}

// init registers the fmt command
func init() {
	fmtCmd.Flags().BoolVar(&flagFmtCheck, "check", false, "fail instead of rewriting files")
	rootCmd.AddCommand(fmtCmd)
}
