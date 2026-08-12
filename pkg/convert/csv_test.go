package convert_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"

	"github.com/theopenlane/courier/pkg/controlfile"
	"github.com/theopenlane/courier/pkg/convert"
)

func TestCSVControls(t *testing.T) {
	input := strings.Join([]string{
		`id,Ref Code,title,description,category,subcategory,tags,parentRefCode,mappedControls.SOC 2,mappedControls.ISO 27001`,
		`CTL_01ABC,CC1.1,Integrity,"Commitment to integrity, and ethical values",Control Environment,Integrity and Ethics,security|governance,,CC1.1,A.5.1`,
		`,CC1.1.3,Acknowledgment,New hires complete an acknowledgment form,Control Environment,Integrity and Ethics,,CC1.1,CC1.1,`,
		`,CC6.1,,,Logical Access,,,,,`,
	}, "\n")

	controls, err := convert.CSV[controlfile.Control](strings.NewReader(input), convert.CSVOptions{})
	assert.NilError(t, err)

	expected := []*controlfile.Control{
		{
			ID:          "CTL_01ABC",
			RefCode:     "CC1.1",
			Title:       "Integrity",
			Description: "Commitment to integrity, and ethical values",
			Category:    "Control Environment",
			Subcategory: "Integrity and Ethics",
			Tags:        []string{"security", "governance"},
			MappedControls: controlfile.MappedControls{
				"SOC 2":     {"CC1.1"},
				"ISO 27001": {"A.5.1"},
			},
			Subcontrols: []*controlfile.Subcontrol{
				{
					RefCode:     "CC1.1.3",
					Title:       "Acknowledgment",
					Description: "New hires complete an acknowledgment form",
					Category:    "Control Environment",
					Subcategory: "Integrity and Ethics",
					MappedControls: controlfile.MappedControls{
						"SOC 2": {"CC1.1"},
					},
				},
			},
		},
		{
			RefCode:  "CC6.1",
			Category: "Logical Access",
		},
	}

	assert.DeepEqual(t, expected, controls)
}

// TestCSVPolicies converts a different document type through the same code
func TestCSVPolicies(t *testing.T) {
	input := "name,policy type,markdownPath,tags\nAccess Control Policy,Security,policies/access-control.md,security|access\n"

	policies, err := convert.CSV[controlfile.Policy](strings.NewReader(input), convert.CSVOptions{})
	assert.NilError(t, err)

	assert.DeepEqual(t, policies, []*controlfile.Policy{
		{
			Name:         "Access Control Policy",
			PolicyType:   "Security",
			MarkdownPath: "policies/access-control.md",
			Tags:         []string{"security", "access"},
		},
	})

	// a control column is not a policy column
	_, err = convert.CSV[controlfile.Policy](strings.NewReader("name,refCode\nAccess,CC1.1\n"), convert.CSVOptions{})
	assert.ErrorIs(t, err, convert.ErrCSVUnknownColumn)

	// the key field follows the type, a policy is identified by its name
	_, err = convert.CSV[controlfile.Policy](strings.NewReader("name,policyType\n,Security\n"), convert.CSVOptions{})
	assert.ErrorIs(t, err, convert.ErrMissingKey)
}

// TestCSVFrontmatter converts a type whose map field is not mappedControls
func TestCSVFrontmatter(t *testing.T) {
	input := "title,status,revision,satisfies.SOC 2\nAccess Control,PUBLISHED,v1.0.0,CC6.1|CC6.2\n"

	frontmatter, err := convert.CSV[controlfile.Frontmatter](strings.NewReader(input), convert.CSVOptions{})
	assert.NilError(t, err)

	assert.DeepEqual(t, frontmatter, []*controlfile.Frontmatter{
		{
			Title:     "Access Control",
			Status:    "PUBLISHED",
			Revision:  "v1.0.0",
			Satisfies: controlfile.MappedControls{"SOC 2": {"CC6.1", "CC6.2"}},
		},
	})
}

func TestCSVToYAMLRoundTrip(t *testing.T) {
	input := "refCode,title,mappedControls.SOC 2\nCC1.1,Integrity,CC1.1|CC1.2\n"

	data, err := convert.CSVToYAML[controlfile.Control](strings.NewReader(input), convert.CSVOptions{})
	assert.NilError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Control](data)
	assert.NilError(t, err)
	assert.NilError(t, controlfile.Validate(parsed))

	assert.Assert(t, is.Len(parsed, 1))
	assert.Equal(t, parsed[0].RefCode, "CC1.1")
	assert.DeepEqual(t, parsed[0].MappedControls["SOC 2"], []string{"CC1.1", "CC1.2"})
}

func TestCSVColumnMapping(t *testing.T) {
	input := "control id,category,refCode,description,owner,spreadsheet notes\nCC01.01,Governance,JH-07,Core values are communicated,Security,needs review\n"

	// a CSV whose columns do not name the document fields maps them itself
	controls, err := convert.CSV[controlfile.Control](strings.NewReader(input), convert.CSVOptions{
		Columns: map[string]string{
			"control id":        "categoryID",
			"owner":             "controlOwner",
			"spreadsheet notes": convert.CSVSkipColumn,
		},
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, controls, []*controlfile.Control{
		{
			RefCode:      "JH-07",
			Description:  "Core values are communicated",
			Category:     "Governance",
			CategoryID:   "CC01.01",
			ControlOwner: "Security",
		},
	})

	// columns naming no field are dropped by option instead
	controls, err = convert.CSV[controlfile.Control](strings.NewReader(input), convert.CSVOptions{
		SkipUnknownColumns: true,
	})
	assert.NilError(t, err)

	assert.DeepEqual(t, controls, []*controlfile.Control{
		{
			RefCode:     "JH-07",
			Description: "Core values are communicated",
			Category:    "Governance",
		},
	})
}

// TestCSVKeyField nests records under a column other than the schema key
func TestCSVKeyField(t *testing.T) {
	input := "refCode,title,parentTitle\nCC1.1,Integrity,\nCC1.1.3,Acknowledgment,Integrity\n"

	controls, err := convert.CSV[controlfile.Control](strings.NewReader(input), convert.CSVOptions{KeyField: "title"})
	assert.NilError(t, err)

	assert.Assert(t, is.Len(controls, 1))
	assert.Assert(t, is.Len(controls[0].Subcontrols, 1))
	assert.Equal(t, controls[0].Subcontrols[0].RefCode, "CC1.1.3")

	_, err = convert.CSV[controlfile.Control](strings.NewReader(input), convert.CSVOptions{KeyField: "nope"})
	assert.ErrorIs(t, err, convert.ErrUnknownKeyField)
}

func TestCSVErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   error
	}{
		{
			name:  "unknown column",
			input: "refCode,owner\nCC1.1,someone\n",
			err:   convert.ErrCSVUnknownColumn,
		},
		{
			name:  "duplicate column",
			input: "refCode,ref code\nCC1.1,CC1.1\n",
			err:   convert.ErrCSVDuplicateColumn,
		},
		{
			name:  "map column without a key",
			input: "refCode,mappedControls.\nCC1.1,CC1.1\n",
			err:   convert.ErrCSVMissingMapKey,
		},
		{
			name:  "map column on a field that is not a map",
			input: "refCode,title.first\nCC1.1,Integrity\n",
			err:   convert.ErrCSVNotAMap,
		},
		{
			name:  "missing key",
			input: "refCode,title\n,Integrity\n",
			err:   convert.ErrMissingKey,
		},
		{
			name:  "duplicate key",
			input: "refCode\nCC1.1\nCC1.1\n",
			err:   convert.ErrDuplicateKey,
		},
		{
			name:  "nested record without its parent",
			input: "refCode,parentRefCode\nCC1.1.3,CC1.1\n",
			err:   convert.ErrCSVUnknownParent,
		},
		{
			name:  "record longer than the header",
			input: "refCode\nCC1.1,extra\n",
			err:   convert.ErrCSVFieldCount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convert.CSV[controlfile.Control](strings.NewReader(tt.input), convert.CSVOptions{})
			assert.ErrorIs(t, err, tt.err)
		})
	}

	_, err := convert.CSV[controlfile.Control](strings.NewReader(""), convert.CSVOptions{})
	assert.ErrorIs(t, err, convert.ErrCSVMissingHeader)
}

// TestConvertFile converts the checked in example inventory through the file
// level entry point the cli uses
func TestConvertFile(t *testing.T) {
	path := filepath.Join("..", "..", "data", "input", "csv", "new", "controls.csv")

	kind, ok := convert.KindForFile(path)
	assert.Assert(t, ok)
	assert.Equal(t, kind, convert.KindControls)

	file, err := os.Open(path)
	assert.NilError(t, err)

	defer file.Close()

	// every column of the example names a field of the control schema
	data, err := convert.Convert(path, file, kind, convert.CSVOptions{})
	assert.NilError(t, err)

	controls, err := controlfile.Unmarshal[controlfile.Control](data)
	assert.NilError(t, err)
	assert.Assert(t, is.Len(controls, 73))

	assert.Equal(t, controls[1].RefCode, "JH-07")
	assert.Equal(t, controls[1].Category, "Organizational Governance and Structure")
	assert.Equal(t, controls[1].CategoryID, "CC01.01")
	assert.Equal(t, controls[1].ControlOwner, "Greg Field")

	// the store file follows the kind, and an unknown format is rejected
	storeFile, err := convert.StoreFile(kind)
	assert.NilError(t, err)
	assert.Equal(t, storeFile, controlfile.ControlsFile)

	_, err = convert.Convert("controls.json", strings.NewReader(""), kind, convert.CSVOptions{})
	assert.ErrorIs(t, err, convert.ErrUnknownFormat)
}
