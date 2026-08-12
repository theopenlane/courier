package engine

import (
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// archiveStore is an inventory holding one control by ID and one by refCode
func archiveStore() *Store {
	return &Store{
		Controls: []*controlfile.Control{
			{ID: "CTL_01ABC", RefCode: "JH-07"},
			{
				RefCode:     "jh-09",
				Subcontrols: []*controlfile.Subcontrol{{RefCode: "JH-09.1"}},
			},
		},
	}
}

func archiveRemote() []RemoteControl {
	return []RemoteControl{
		// held by the store under its Openlane ID, even though it was renamed
		{ID: "CTL_01ABC", RefCode: "JH-07-renamed", Title: "Core Values"},
		// held by refCode, the API resolves those case-insensitively
		{ID: "CTL_02DEF", RefCode: "JH-09", Title: "Board Independence"},
		// no longer in the store, a candidate to archive
		{ID: "CTL_03GHI", RefCode: "JH-19", Title: "Vendor Risk", Status: "APPROVED"},
		// also gone, but derived from a standard and never ours to retire
		{ID: "CTL_04JKL", RefCode: "CC6.7", Title: "Endpoint Access", ReferenceFramework: "SOC 2"},
	}
}

func TestArchivable(t *testing.T) {
	result := archivable(archiveRemote(), nil, heldRecords(archiveStore()))

	// the framework control is not compared at all, so it is not in the total
	assert.Equal(t, result.TotalRemote, 3)

	assert.DeepEqual(t, result.Controls, []ArchivableControl{
		{ID: "CTL_03GHI", RefCode: "JH-19", Title: "Vendor Risk", Status: "APPROVED"},
	})
}

func TestArchivableEmptyStore(t *testing.T) {
	// an empty inventory makes every org-owned control archivable, and still
	// never reports the framework-derived one
	result := archivable(archiveRemote(), nil, heldRecords(&Store{}))

	assert.Assert(t, is.Len(result.Controls, 3))

	for _, control := range result.Controls {
		assert.Assert(t, control.RefCode != "CC6.7")
	}
}

func TestArchivableSubcontrols(t *testing.T) {
	subcontrols := []RemoteSubcontrol{
		// held by the store under its parent
		{RemoteControl: RemoteControl{ID: "SCL_01", RefCode: "JH-09.1"}, ControlID: "CTL_02DEF"},
		// gone from the store
		{RemoteControl: RemoteControl{ID: "SCL_02", RefCode: "JH-09.2", Title: "Quarterly Review"}, ControlID: "CTL_02DEF"},
		// framework derived, never reported
		{RemoteControl: RemoteControl{ID: "SCL_03", RefCode: "CC6.7.1", ReferenceFramework: "SOC 2"}, ControlID: "CTL_02DEF"},
		// parented by a control the org does not own, so out of scope
		{RemoteControl: RemoteControl{ID: "SCL_04", RefCode: "X.1"}, ControlID: "CTL_99ZZZ"},
	}

	result := archivable(archiveRemote(), subcontrols, heldRecords(archiveStore()))

	assert.DeepEqual(t, result.Controls, []ArchivableControl{
		{ID: "CTL_03GHI", RefCode: "JH-19", Title: "Vendor Risk", Status: "APPROVED"},
		{
			ID:            "SCL_02",
			RefCode:       "JH-09.2",
			Title:         "Quarterly Review",
			Subcontrol:    true,
			ParentRefCode: "JH-09",
		},
	})

	// three controls plus the two subcontrols of an org-owned parent
	assert.Equal(t, result.TotalRemote, 5)
}

// TestArchivableSubcontrolRefCodesRepeat keeps subcontrols of different
// parents apart, their refCodes are only unique within a control
func TestArchivableSubcontrolRefCodesRepeat(t *testing.T) {
	store := &Store{
		Controls: []*controlfile.Control{
			{
				ID:          "CTL_01ABC",
				RefCode:     "JH-07",
				Subcontrols: []*controlfile.Subcontrol{{RefCode: "1.1"}},
			},
			{ID: "CTL_02DEF", RefCode: "JH-09"},
		},
	}

	controls := []RemoteControl{
		{ID: "CTL_01ABC", RefCode: "JH-07"},
		{ID: "CTL_02DEF", RefCode: "JH-09"},
	}

	subcontrols := []RemoteSubcontrol{
		{RemoteControl: RemoteControl{ID: "SCL_01", RefCode: "1.1"}, ControlID: "CTL_01ABC"},
		{RemoteControl: RemoteControl{ID: "SCL_02", RefCode: "1.1"}, ControlID: "CTL_02DEF"},
	}

	result := archivable(controls, subcontrols, heldRecords(store))

	// only the one under JH-09 is archivable, the store never declared it
	assert.Assert(t, is.Len(result.Controls, 1))
	assert.Equal(t, result.Controls[0].ID, "SCL_02")
	assert.Equal(t, result.Controls[0].ParentRefCode, "JH-09")
}
