package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/convert"
)

var (
	// flagConvertKind is the document kind the input converts into
	flagConvertKind string
	// flagConvertOutput is the file the converted yaml is written to
	flagConvertOutput string
	// flagConvertWrite writes the converted yaml into the store directory
	flagConvertWrite bool
	// flagConvertForce allows overwriting an existing output file
	flagConvertForce bool
	// flagConvertColumns maps input columns onto document fields
	flagConvertColumns map[string]string
	// flagConvertSkipUnknown drops columns naming no document field
	flagConvertSkipUnknown bool
	// flagConvertKeyField identifies a record by a field other than the schema key
	flagConvertKeyField string
)

// convertCmd turns an external input into a store file
var convertCmd = &cobra.Command{
	Use:   "convert [file]",
	Short: "convert a csv into a store yaml file",
	Long: `convert turns a csv into the yaml courier keeps in git, rendered exactly as
pull would write it.

The csv header names the fields to fill, case insensitively and ignoring
spaces, dashes, and underscores, so refCode, ref_code, and "Ref Code" are the
same field. A column named <field>.<key> fills one key of a map field, e.g.
mappedControls.SOC 2, and multiple values in one cell separate with a pipe. A
parentRefCode column nests a control under a control declared in an earlier
row. Columns naming no field are an error, so use --map to map them onto a
field, --map <column>=- to drop one, or --skip-unknown to drop them all.

The output goes to stdout unless --output or --write is given.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runConvert(args[0])
	},
}

// runConvert converts the input file and writes the result
func runConvert(input string) error {
	kind, err := convertKind(input)
	if err != nil {
		return err
	}

	file, err := os.Open(input)
	if err != nil {
		return err
	}

	defer file.Close()

	data, err := convert.Convert(input, file, kind, convert.CSVOptions{
		Columns:            flagConvertColumns,
		SkipUnknownColumns: flagConvertSkipUnknown,
		KeyField:           flagConvertKeyField,
	})
	if err != nil {
		return err
	}

	output, err := resolveOutput(kind, flagConvertOutput, flagConvertWrite)
	if err != nil {
		return err
	}

	if output == "" {
		fmt.Print(string(data))

		return nil
	}

	if err := writeOutput(output, data, flagConvertForce); err != nil {
		return err
	}

	fmt.Printf("converted %s into %s\n", input, output)

	return nil
}

// convertKind resolves the document kind to convert into, the input file name
// names it when --kind is not given
func convertKind(input string) (convert.Kind, error) {
	if flagConvertKind != "" {
		if _, err := convert.StoreFile(convert.Kind(flagConvertKind)); err != nil {
			return "", err
		}

		return convert.Kind(flagConvertKind), nil
	}

	kind, ok := convert.KindForFile(input)
	if !ok {
		return "", fmt.Errorf("%w: name the kind with --kind, one of %s", ErrUnknownConvertKind, convertKinds())
	}

	return kind, nil
}

// convertKinds lists the convertible kinds for the help text
func convertKinds() string {
	return strings.Join(lo.Map(convert.Kinds(), func(k convert.Kind, _ int) string { return string(k) }), ", ")
}

// init registers the convert command
func init() {
	convertCmd.Flags().StringVar(&flagConvertKind, "kind", "", "document kind to convert into ("+convertKinds()+"), defaults to the input file name")
	convertCmd.Flags().StringVarP(&flagConvertOutput, "output", "o", "", "file to write the converted yaml to (default stdout)")
	convertCmd.Flags().BoolVar(&flagConvertWrite, "write", false, "write the converted yaml into the store directory")
	convertCmd.Flags().BoolVar(&flagConvertForce, "force", false, "replace the output file when it already exists")
	convertCmd.Flags().StringToStringVar(&flagConvertColumns, "map", nil, "map an input column onto a document field, e.g. --map categoryID=subcategory, or to - to drop it")
	convertCmd.Flags().BoolVar(&flagConvertSkipUnknown, "skip-unknown", false, "drop input columns naming no document field")
	convertCmd.Flags().StringVar(&flagConvertKeyField, "key-field", "", "field identifying a record, defaults to the field the schema marks required")

	rootCmd.AddCommand(convertCmd)
}
