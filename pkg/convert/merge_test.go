package convert_test

import (
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"

	"github.com/theopenlane/courier/pkg/controlfile"
	"github.com/theopenlane/courier/pkg/convert"
)

func TestMergeFillsEmptyFields(t *testing.T) {
	primary := []*controlfile.Control{
		{
			ID:          "CTL_01ABC",
			RefCode:     "CC1.1",
			Title:       "Integrity",
			Description: "",
			Category:    "Control Environment",
			Tags:        []string{"security"},
			MappedControls: controlfile.MappedControls{
				"SOC 2": {"CC1.1"},
			},
		},
	}

	secondary := []*controlfile.Control{
		{
			RefCode:      "CC1.1",
			Title:        "Integrity and Ethical Values",
			Description:  "The organization demonstrates a commitment to integrity",
			Category:     "Ignored, the primary already fills this",
			ControlOwner: "Security Team",
			Tags:         []string{"governance"},
			MappedControls: controlfile.MappedControls{
				"SOC 2":     {"CC1.2"},
				"ISO 27001": {"A.5.1"},
			},
		},
	}

	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 1))

	assert.DeepEqual(t, merged[0], &controlfile.Control{
		ID:      "CTL_01ABC",
		RefCode: "CC1.1",
		// the primary wins every field it fills
		Title:    "Integrity",
		Category: "Control Environment",
		Tags:     []string{"security"},
		// and takes what it leaves empty from the secondary
		Description:  "The organization demonstrates a commitment to integrity",
		ControlOwner: "Security Team",
		// map fields merge key by key, the primary keeps SOC 2
		MappedControls: controlfile.MappedControls{
			"SOC 2":     {"CC1.1"},
			"ISO 27001": {"A.5.1"},
		},
	})

	assert.DeepEqual(t, report.Filled, []string{
		"CC1.1.description",
		"CC1.1.controlOwner",
		"CC1.1.mappedControls.ISO 27001",
	})

	assert.Assert(t, is.Len(report.Added, 0))
}

func TestMergeRecords(t *testing.T) {
	primary := []*controlfile.Control{{RefCode: "CC1.1"}}
	secondary := []*controlfile.Control{
		{RefCode: "CC6.1", Title: "Logical Access"},
		{RefCode: "CC1.1", Title: "Integrity"},
	}

	// records the primary does not hold are appended in their own order
	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 2))
	assert.Equal(t, merged[0].RefCode, "CC1.1")
	assert.Equal(t, merged[0].Title, "Integrity")
	assert.Equal(t, merged[1].RefCode, "CC6.1")
	assert.DeepEqual(t, report.Added, []string{"CC6.1"})

	// or left out, filling only what the primary already tracks
	primary = []*controlfile.Control{{RefCode: "CC1.1"}}

	merged, report, err = convert.Merge(primary, secondary, convert.MergeOptions{SkipNewRecords: true})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 1))
	assert.Equal(t, merged[0].Title, "Integrity")
	assert.DeepEqual(t, report.Skipped, []string{"CC6.1"})
}

func TestMergeNestedRecords(t *testing.T) {
	primary := []*controlfile.Control{
		{
			RefCode:     "CC1.1",
			Subcontrols: []*controlfile.Subcontrol{{RefCode: "CC1.1.1", Title: "Code of Conduct"}},
		},
	}

	secondary := []*controlfile.Control{
		{
			RefCode: "CC1.1",
			Subcontrols: []*controlfile.Subcontrol{
				{RefCode: "CC1.1.1", Title: "Ignored", Description: "A code of conduct is published"},
				{RefCode: "CC1.1.3", Title: "New Hire Acknowledgment"},
			},
		},
	}

	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged[0].Subcontrols, 2))

	// a matched subcontrol merges by the same rules
	assert.Equal(t, merged[0].Subcontrols[0].Title, "Code of Conduct")
	assert.Equal(t, merged[0].Subcontrols[0].Description, "A code of conduct is published")

	// and an unmatched one is appended under its parent
	assert.Equal(t, merged[0].Subcontrols[1].RefCode, "CC1.1.3")

	assert.DeepEqual(t, report.Filled, []string{"CC1.1.CC1.1.1.description"})
	assert.DeepEqual(t, report.Added, []string{"CC1.1.CC1.1.3"})
}

// TestMergePolicies merges a different document type through the same code,
// policies are identified by name rather than refCode
func TestMergePolicies(t *testing.T) {
	primary := []*controlfile.Policy{{ID: "PLC_01ABC", Name: "Access Control Policy"}}
	secondary := []*controlfile.Policy{{Name: "Access Control Policy", PolicyType: "Security"}}

	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)

	assert.DeepEqual(t, merged, []*controlfile.Policy{
		{
			ID:         "PLC_01ABC",
			Name:       "Access Control Policy",
			PolicyType: "Security",
		},
	})

	assert.DeepEqual(t, report.Filled, []string{"Access Control Policy.policyType"})
}

func TestMergeYAML(t *testing.T) {
	primary := []byte("- refCode: CC1.1\n  title: Integrity\n  description: \"\"\n")
	secondary := []byte("- refCode: CC1.1\n  title: Ignored\n  description: Commitment to integrity\n- refCode: CC6.1\n  title: Logical Access\n")

	data, report, err := convert.MergeYAML[controlfile.Control](primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Control](data)
	assert.NilError(t, err)
	assert.Assert(t, is.Len(parsed, 2))
	assert.Equal(t, parsed[0].Title, "Integrity")
	assert.Equal(t, parsed[0].Description, "Commitment to integrity")
	assert.DeepEqual(t, report.Added, []string{"CC6.1"})

	// the same merge through the kind registry the cli uses
	viaKind, _, err := convert.MergeStoreFiles(convert.KindControls, primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Equal(t, string(viaKind), string(data))

	_, _, err = convert.MergeStoreFiles("nope", primary, secondary, convert.MergeOptions{})
	assert.ErrorIs(t, err, convert.ErrUnknownKind)
}

// TestMergeKeyField matches records by a field other than the schema key
func TestMergeKeyField(t *testing.T) {
	primary := []*controlfile.Control{{RefCode: "JH-07", ReferenceID: "INT-1"}}
	secondary := []*controlfile.Control{{RefCode: "CC1.1", ReferenceID: "INT-1", Title: "Integrity"}}

	merged, _, err := convert.Merge(primary, secondary, convert.MergeOptions{KeyField: "referenceID"})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 1))
	assert.Equal(t, merged[0].RefCode, "JH-07")
	assert.Equal(t, merged[0].Title, "Integrity")
}

func TestMergeErrors(t *testing.T) {
	// a set that identifies two records the same way cannot be merged
	primary := []*controlfile.Control{{RefCode: "CC1.1"}, {RefCode: "CC1.1"}}

	_, _, err := convert.Merge(primary, nil, convert.MergeOptions{})
	assert.ErrorIs(t, err, convert.ErrDuplicateKey)

	// nor can a record that does not fill the field it is identified by
	_, _, err = convert.Merge([]*controlfile.Control{{Title: "Integrity"}}, nil, convert.MergeOptions{})
	assert.ErrorIs(t, err, convert.ErrMissingKey)

	_, _, err = convert.Merge[controlfile.Control](nil, nil, convert.MergeOptions{KeyField: "nope"})
	assert.ErrorIs(t, err, convert.ErrUnknownKeyField)

	// a type with no field identifying a record cannot be matched at all
	_, _, err = convert.Merge([]*controlfile.Frontmatter{{Title: "Access"}}, nil, convert.MergeOptions{})
	assert.ErrorIs(t, err, convert.ErrNoKeyField)
}

// TestMergeMatchesOnID matches records the way apply matches them against
// Openlane, on the server assigned id before the refCode
func TestMergeMatchesOnID(t *testing.T) {
	// the control was renamed in Openlane after the spreadsheet was exported
	primary := []*controlfile.Control{
		{ID: "CTL_01ABC", RefCode: "CC1.1-renamed", Category: "Control Environment"},
	}

	secondary := []*controlfile.Control{
		{ID: "CTL_01ABC", RefCode: "CC1.1", Description: "Commitment to integrity"},
	}

	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 1))

	// the id matched, so the rename holds and the description fills in
	assert.Equal(t, merged[0].RefCode, "CC1.1-renamed")
	assert.Equal(t, merged[0].Description, "Commitment to integrity")
	assert.DeepEqual(t, report.Filled, []string{"CC1.1-renamed.description"})

	// matching on the key alone reads the rename as a new control
	primary = []*controlfile.Control{
		{ID: "CTL_01ABC", RefCode: "CC1.1-renamed", Category: "Control Environment"},
	}

	merged, report, err = convert.Merge(primary, secondary, convert.MergeOptions{MatchKeyOnly: true})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 2))
	assert.DeepEqual(t, report.Added, []string{"CC1.1"})
}

// TestMergeFallsBackToKey matches on the refCode when the ids do not line up,
// which is every record of a converted csv
func TestMergeFallsBackToKey(t *testing.T) {
	primary := []*controlfile.Control{
		{ID: "CTL_01ABC", RefCode: "JH-07"},
		{RefCode: "JH-09"},
	}

	// a converted csv carries no ids at all
	secondary := []*controlfile.Control{
		{RefCode: "JH-07", Description: "Core values are communicated"},
		{RefCode: "JH-09", Description: "The board operates independently"},
	}

	merged, report, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 2))
	assert.Equal(t, merged[0].Description, "Core values are communicated")
	assert.Equal(t, merged[1].Description, "The board operates independently")
	assert.Assert(t, is.Len(report.Added, 0))

	// an id the primary has never seen falls back to the refCode too
	primary = []*controlfile.Control{{ID: "CTL_01ABC", RefCode: "JH-07"}}
	secondary = []*controlfile.Control{{ID: "CTL_09XYZ", RefCode: "JH-07", Title: "Core Values"}}

	merged, _, err = convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged, 1))
	assert.Equal(t, merged[0].ID, "CTL_01ABC")
	assert.Equal(t, merged[0].Title, "Core Values")
}

// TestMergeSubcontrolsMatchOnID nests the same matching under a parent
func TestMergeSubcontrolsMatchOnID(t *testing.T) {
	primary := []*controlfile.Control{
		{
			RefCode:     "CC1.1",
			Subcontrols: []*controlfile.Subcontrol{{ID: "SCL_01ABC", RefCode: "CC1.1.1-renamed"}},
		},
	}

	secondary := []*controlfile.Control{
		{
			RefCode:     "CC1.1",
			Subcontrols: []*controlfile.Subcontrol{{ID: "SCL_01ABC", RefCode: "CC1.1.1", Title: "Code of Conduct"}},
		},
	}

	merged, _, err := convert.Merge(primary, secondary, convert.MergeOptions{})
	assert.NilError(t, err)
	assert.Assert(t, is.Len(merged[0].Subcontrols, 1))
	assert.Equal(t, merged[0].Subcontrols[0].RefCode, "CC1.1.1-renamed")
	assert.Equal(t, merged[0].Subcontrols[0].Title, "Code of Conduct")
}

func TestMergeDuplicateID(t *testing.T) {
	primary := []*controlfile.Control{
		{ID: "CTL_01ABC", RefCode: "CC1.1"},
		{ID: "CTL_01ABC", RefCode: "CC6.1"},
	}

	_, _, err := convert.Merge(primary, nil, convert.MergeOptions{})
	assert.ErrorIs(t, err, convert.ErrDuplicateID)

	_, _, err = convert.Merge(primary, nil, convert.MergeOptions{IDField: "nope"})
	assert.ErrorIs(t, err, convert.ErrUnknownIDField)
}
