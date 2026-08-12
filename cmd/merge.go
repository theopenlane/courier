package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/convert"
)

var (
	// flagMergeKind is the document kind both files hold
	flagMergeKind string
	// flagMergeOutput is the file the merged yaml is written to
	flagMergeOutput string
	// flagMergeWrite writes the merged yaml into the store directory
	flagMergeWrite bool
	// flagMergeForce allows overwriting an existing output file
	flagMergeForce bool
	// flagMergeOnlyExisting leaves out records only the secondary file holds
	flagMergeOnlyExisting bool
	// flagMergeKeyField matches records by a field other than the schema key
	flagMergeKeyField string
	// flagMergeMatchKeyOnly ignores server assigned ids when matching records
	flagMergeMatchKeyOnly bool
)

// mergeCmd combines two store files into one
var mergeCmd = &cobra.Command{
	Use:   "merge [primary] [secondary]",
	Short: "merge two store yaml files, the primary winning field by field",
	Long: `merge combines two store files into one. The primary file wins: a field it
fills is kept, and a field it leaves empty takes the value the secondary file
holds for the same record.

Records match the way apply matches them against Openlane: on the id the
server assigned when both records carry one, and on refCode for controls or
name for policies otherwise, so a record renamed in Openlane still merges with
the record it came from. Pass --match-key-only to match on the key alone.
Map fields such as mappedControls merge key by key, so the
secondary file contributes only the frameworks the primary is missing, and
subcontrols merge under their parent by the same rules. Records only the
secondary file holds are appended unless --only-existing is given.

Use it to bring a converted csv into what you already pulled from Openlane:
the pulled file is the primary, the converted one fills in its gaps.

The output goes to stdout unless --output or --write is given.`,
	Args: cobra.ExactArgs(2), //nolint:mnd // a primary and a secondary file
	RunE: func(_ *cobra.Command, args []string) error {
		return runMerge(args[0], args[1])
	},
}

// runMerge merges the two files and writes the result
func runMerge(primaryPath, secondaryPath string) error {
	kind, err := mergeKind(primaryPath, secondaryPath)
	if err != nil {
		return err
	}

	primary, err := os.ReadFile(primaryPath)
	if err != nil {
		return err
	}

	secondary, err := os.ReadFile(secondaryPath)
	if err != nil {
		return err
	}

	data, report, err := convert.MergeStoreFiles(kind, primary, secondary, convert.MergeOptions{
		KeyField:       flagMergeKeyField,
		SkipNewRecords: flagMergeOnlyExisting,
		MatchKeyOnly:   flagMergeMatchKeyOnly,
	})
	if err != nil {
		return err
	}

	output, err := resolveOutput(kind, flagMergeOutput, flagMergeWrite)
	if err != nil {
		return err
	}

	if output == "" {
		fmt.Print(string(data))

		return nil
	}

	if err := writeOutput(output, data, flagMergeForce); err != nil {
		return err
	}

	printList("filled", report.Filled)
	printList("added", report.Added)
	printList("skipped", report.Skipped)

	if len(report.Filled)+len(report.Added) == 0 {
		fmt.Printf("%s took nothing from %s\n", primaryPath, secondaryPath)

		return nil
	}

	fmt.Printf("merged %s into %s\n", secondaryPath, output)

	return nil
}

// mergeKind resolves the document kind both files hold, either file name
// names it when --kind is not given
func mergeKind(primary, secondary string) (convert.Kind, error) {
	if flagMergeKind != "" {
		if _, err := convert.StoreFile(convert.Kind(flagMergeKind)); err != nil {
			return "", err
		}

		return convert.Kind(flagMergeKind), nil
	}

	for _, path := range []string{primary, secondary} {
		if kind, ok := convert.KindForFile(path); ok {
			return kind, nil
		}
	}

	return "", fmt.Errorf("%w: name the kind with --kind, one of %s", ErrUnknownConvertKind, convertKinds())
}

// init registers the merge command
func init() {
	mergeCmd.Flags().StringVar(&flagMergeKind, "kind", "", "document kind both files hold ("+convertKinds()+"), defaults to the file names")
	mergeCmd.Flags().StringVarP(&flagMergeOutput, "output", "o", "", "file to write the merged yaml to (default stdout)")
	mergeCmd.Flags().BoolVar(&flagMergeWrite, "write", false, "write the merged yaml into the store directory")
	mergeCmd.Flags().BoolVar(&flagMergeForce, "force", false, "replace the output file when it already exists")
	mergeCmd.Flags().BoolVar(&flagMergeOnlyExisting, "only-existing", false, "fill existing records only, leaving out records the primary file does not hold")
	mergeCmd.Flags().StringVar(&flagMergeKeyField, "key-field", "", "field records are matched by, defaults to the field the schema marks required")
	mergeCmd.Flags().BoolVar(&flagMergeMatchKeyOnly, "match-key-only", false, "match records by their key alone, ignoring the ids the server assigned")

	rootCmd.AddCommand(mergeCmd)
}
