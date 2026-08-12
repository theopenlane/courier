package engine

import (
	"context"
	"slices"
	"strings"
)

// ArchivableControl is a control in Openlane that the store no longer holds,
// a candidate for archiving rather than a record courier acts on
type ArchivableControl struct {
	// ID is the Openlane ULID of the control
	ID string `json:"id"`
	// RefCode is the reference code of the control in Openlane
	RefCode string `json:"refCode"`
	// Title is the human readable title of the control
	Title string `json:"title"`
	// Category is the category of the control
	Category string `json:"category"`
	// Status is the status of the control in Openlane, e.g. APPROVED
	Status string `json:"status,omitempty"`
	// Subcontrol reports whether the record is a subcontrol rather than a control
	Subcontrol bool `json:"subcontrol,omitempty"`
	// ParentRefCode is the refCode of the parent control, for a subcontrol
	ParentRefCode string `json:"parentRefCode,omitempty"`
}

// ArchivableResult reports the controls the store no longer holds
type ArchivableResult struct {
	// Controls are the archivable controls and subcontrols, controls first
	// and each group ordered by refCode
	Controls []ArchivableControl `json:"controls,omitempty"`
	// TotalRemote is the number of org-owned records without a reference
	// framework that were compared against the store
	TotalRemote int `json:"totalRemote"`
}

// Archivable lists the organization's controls that exist in Openlane and are
// no longer in the store, the records to consider archiving after an inventory
// has been rewritten from a spreadsheet.
//
// Only controls without a reference framework are considered: a control that
// derives from a standard is not yours to retire and is never reported, even
// when the store does not carry it. Records are matched the way apply matches
// them, on the Openlane ID the store carries and on refCode otherwise, so a
// control the store still holds under either identity is not reported.
//
// Nothing is archived here, courier never deletes, the result is a report
func (c *Client) Archivable(ctx context.Context, store *Store, includeSubcontrols bool) (*ArchivableResult, error) {
	controls, err := c.fetchControls(ctx, organizationControlsWhere())
	if err != nil {
		return nil, err
	}

	var subcontrols []RemoteSubcontrol

	if includeSubcontrols {
		if subcontrols, err = c.fetchSubcontrols(ctx); err != nil {
			return nil, err
		}
	}

	return archivable(controls, subcontrols, heldRecords(store)), nil
}

// archivable compares the records Openlane holds against the identities the
// store carries, it is the whole of the report and does no fetching
func archivable(controls []RemoteControl, subcontrols []RemoteSubcontrol, held *heldSet) *ArchivableResult {
	result := &ArchivableResult{}

	parents := map[string]RemoteControl{}

	for _, remote := range controls {
		if remote.ReferenceFramework != "" {
			continue
		}

		parents[remote.ID] = remote
		result.TotalRemote++

		if held.holds(remote.ID, remote.RefCode) {
			continue
		}

		result.Controls = append(result.Controls, ArchivableControl{
			ID:       remote.ID,
			RefCode:  remote.RefCode,
			Title:    remote.Title,
			Category: remote.Category,
			Status:   remote.Status,
		})
	}

	slices.SortFunc(result.Controls, byRefCode)

	result.Controls = append(result.Controls, archivableNested(subcontrols, parents, held, result)...)

	return result
}

// archivableNested lists the subcontrols the store no longer holds, scoped to
// the parents the org owns so a subcontrol of a framework control, or of a
// control the org does not own, is never reported
func archivableNested(subcontrols []RemoteSubcontrol, parents map[string]RemoteControl, held *heldSet, result *ArchivableResult) []ArchivableControl {
	var records []ArchivableControl

	for _, remote := range subcontrols {
		parent, ok := parents[remote.ControlID]
		if !ok || remote.ReferenceFramework != "" {
			continue
		}

		result.TotalRemote++

		if held.holdsNested(parent.RefCode, remote.ID, remote.RefCode) {
			continue
		}

		records = append(records, ArchivableControl{
			ID:            remote.ID,
			RefCode:       remote.RefCode,
			Title:         remote.Title,
			Category:      remote.Category,
			Status:        remote.Status,
			Subcontrol:    true,
			ParentRefCode: parent.RefCode,
		})
	}

	slices.SortFunc(records, byRefCode)

	return records
}

// byRefCode orders archivable records by their parent and reference code
func byRefCode(a, b ArchivableControl) int {
	if parents := strings.Compare(a.ParentRefCode, b.ParentRefCode); parents != 0 {
		return parents
	}

	return strings.Compare(a.RefCode, b.RefCode)
}

// heldSet is the identities a store holds, refCodes are matched the way the
// API resolves them, case-insensitively
type heldSet struct {
	ids      map[string]struct{}
	refCodes map[string]struct{}
	// nested holds subcontrol identities keyed by their parent refCode, since
	// a subcontrol refCode is only unique within its control
	nested map[string]struct{}
}

// holds reports whether the store carries a record under either identity
func (h *heldSet) holds(id, refCode string) bool {
	if _, ok := h.ids[id]; ok && id != "" {
		return true
	}

	_, ok := h.refCodes[strings.ToLower(refCode)]

	return ok
}

// holdsNested reports whether the store carries a subcontrol of a parent
func (h *heldSet) holdsNested(parentRefCode, id, refCode string) bool {
	if _, ok := h.ids[id]; ok && id != "" {
		return true
	}

	_, ok := h.nested[nestedKey(parentRefCode, refCode)]

	return ok
}

// nestedKey identifies a subcontrol by its parent and reference code
func nestedKey(parentRefCode, refCode string) string {
	return strings.ToLower(parentRefCode) + "::" + strings.ToLower(refCode)
}

// heldRecords collects the identities the store's control inventory holds
func heldRecords(store *Store) *heldSet {
	held := &heldSet{
		ids:      map[string]struct{}{},
		refCodes: map[string]struct{}{},
		nested:   map[string]struct{}{},
	}

	for _, control := range store.Controls {
		add(held.ids, control.ID)
		add(held.refCodes, strings.ToLower(control.RefCode))

		for _, sub := range control.Subcontrols {
			add(held.ids, sub.ID)
			add(held.nested, nestedKey(control.RefCode, sub.RefCode))
		}
	}

	return held
}

// add records a non-empty identity
func add(set map[string]struct{}, value string) {
	if value != "" {
		set[value] = struct{}{}
	}
}
