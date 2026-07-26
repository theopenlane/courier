package engine

import (
	"slices"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// sanitizer mirrors the sanitization the server applies to uploaded document
// bodies before storing them as details, comparing sanitized values keeps the
// plan stable for content the server has already escaped
var sanitizer = bluemonday.UGCPolicy()

// FieldDiff is a single field-level difference between a workspace entry and the API
type FieldDiff struct {
	// Field is the name of the differing field
	Field string `json:"field"`
	// Old is the current API value rendered for display
	Old string `json:"old"`
	// New is the desired value from the workspace rendered for display
	New string `json:"new"`
}

// ControlCreate is an inventory entry with no matching control in the API
type ControlCreate struct {
	// Doc is the entry to create
	Doc *controlfile.Control `json:"doc"`
}

// ControlUpdate is an inventory entry matched to an existing control with differences
type ControlUpdate struct {
	// Doc is the entry holding the desired state
	Doc *controlfile.Control `json:"doc"`
	// Remote is the current state of the matched control
	Remote RemoteControl `json:"remote"`
	// Diffs are the field-level changes to apply
	Diffs []FieldDiff `json:"diffs"`
}

// MappingAdd records mapped control targets that exist in the workspace but
// not in the API for one control
type MappingAdd struct {
	// RefCode is the refCode of the control the mapping starts from
	RefCode string `json:"refCode"`
	// ControlID is the ID of that control, empty until apply creates it
	ControlID string `json:"controlID,omitempty"`
	// Targets are the refCodes to map the control to
	Targets []string `json:"targets"`
}

// MappingDrift records mapped control targets that exist in the API but not
// in the workspace, they are reported and never removed
type MappingDrift struct {
	// RefCode is the refCode of the control the mapping starts from
	RefCode string `json:"refCode"`
	// Targets are the refCodes mapped in the API but absent from the workspace
	Targets []string `json:"targets"`
}

// PolicyCreate is a manifest entry with no matching policy in the API
type PolicyCreate struct {
	// Policy is the manifest entry to create
	Policy *controlfile.Policy `json:"policy"`
	// Markdown is the raw markdown document uploaded on create
	Markdown []byte `json:"-"`
}

// PolicyUpdate is a manifest entry matched to an existing policy with differences
type PolicyUpdate struct {
	// Policy is the manifest entry holding the desired state
	Policy *controlfile.Policy `json:"policy"`
	// Remote is the current state of the matched policy
	Remote RemotePolicy `json:"remote"`
	// Diffs are the field-level changes to apply
	Diffs []FieldDiff `json:"diffs"`
	// AddControls are mapped control refCodes to link that are not yet linked
	AddControls []string `json:"addControls,omitempty"`
	// BodyChanged reports whether the markdown body differs from the policy details
	BodyChanged bool `json:"bodyChanged"`
	// Markdown is the raw markdown document uploaded when the body changed
	Markdown []byte `json:"-"`
}

// PolicyDrift records policy control links that exist in the API but not in
// the workspace, they are reported and never removed
type PolicyDrift struct {
	// Name is the policy name
	Name string `json:"name"`
	// Targets are the linked control refCodes absent from the workspace
	Targets []string `json:"targets"`
}

// Plan is the full set of differences between a workspace and the API
type Plan struct {
	// CreateControls are inventory entries that will create new controls
	CreateControls []ControlCreate `json:"createControls,omitempty"`
	// UpdateControls are inventory entries that will update existing controls
	UpdateControls []ControlUpdate `json:"updateControls,omitempty"`
	// MappingAdds are mapped control targets that will be created
	MappingAdds []MappingAdd `json:"mappingAdds,omitempty"`
	// CreatePolicies are manifest entries that will create new policies
	CreatePolicies []PolicyCreate `json:"createPolicies,omitempty"`
	// UpdatePolicies are manifest entries that will update existing policies
	UpdatePolicies []PolicyUpdate `json:"updatePolicies,omitempty"`
	// DriftControls exist in the API but not the workspace, reported and never deleted
	DriftControls []RemoteControl `json:"driftControls,omitempty"`
	// DriftMappings are mapped targets present only in the API, reported and never removed
	DriftMappings []MappingDrift `json:"driftMappings,omitempty"`
	// DriftPolicies exist in the API but not the workspace, reported and never deleted
	DriftPolicies []RemotePolicy `json:"driftPolicies,omitempty"`
	// DriftPolicyControls are policy control links present only in the API
	DriftPolicyControls []PolicyDrift `json:"driftPolicyControls,omitempty"`

	// localIDs maps lowercased workspace refCodes to control IDs, populated
	// during matching and extended as apply creates controls
	localIDs map[string]string
}

// HasChanges reports whether applying the plan would modify the API
func (p *Plan) HasChanges() bool {
	return len(p.CreateControls)+len(p.UpdateControls)+len(p.MappingAdds)+
		len(p.CreatePolicies)+len(p.UpdatePolicies) > 0
}

// ComputePlan diffs a workspace against remote state, controls are matched by
// id then refCode, policies by id then name
func ComputePlan(ws *Workspace, state *RemoteState) (*Plan, error) {
	controlfile.NormalizePolicies(ws.Policies)

	if err := controlfile.ValidateControls(ws.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.ValidatePolicies(ws.Policies); err != nil {
		return nil, err
	}

	plan := &Plan{localIDs: map[string]string{}}

	planControls(plan, ws, state)

	if err := planPolicies(plan, ws, state); err != nil {
		return nil, err
	}

	return plan, nil
}

// matchRemote finds the remote record a workspace entry refers to, by ID when
// the entry has one and by case-insensitive key otherwise, records already
// matched to another entry are not reused
func matchRemote[R any](id, key string, byID, byKey map[string]R, consumed map[string]struct{}, idOf func(R) string) (R, bool) {
	if id != "" {
		if r, ok := byID[id]; ok {
			return r, true
		}
	}

	r, ok := byKey[strings.ToLower(key)]
	if !ok {
		return r, false
	}

	if _, taken := consumed[idOf(r)]; taken {
		var zero R

		return zero, false
	}

	return r, true
}

// planControls matches inventory entries against remote controls and computes
// field and mapped control changes
func planControls(plan *Plan, ws *Workspace, state *RemoteState) {
	idOf := func(c RemoteControl) string { return c.ID }
	byID := lo.SliceToMap(state.Controls, func(c RemoteControl) (string, RemoteControl) { return c.ID, c })
	byRefCode := lo.SliceToMap(state.Controls, func(c RemoteControl) (string, RemoteControl) {
		return strings.ToLower(c.RefCode), c
	})

	remoteTargets := remoteMappedTargets(state)
	consumed := map[string]struct{}{}

	for _, doc := range ws.Controls {
		remote, ok := matchRemote(doc.ID, doc.RefCode, byID, byRefCode, consumed, idOf)
		if !ok {
			plan.CreateControls = append(plan.CreateControls, ControlCreate{Doc: doc})

			if len(doc.MappedControls) > 0 {
				plan.MappingAdds = append(plan.MappingAdds, MappingAdd{RefCode: doc.RefCode, Targets: doc.MappedControls})
			}

			continue
		}

		consumed[remote.ID] = struct{}{}
		plan.localIDs[strings.ToLower(doc.RefCode)] = remote.ID

		if diffs := controlDiffs(doc, remote); len(diffs) > 0 {
			plan.UpdateControls = append(plan.UpdateControls, ControlUpdate{Doc: doc, Remote: remote, Diffs: diffs})
		}

		adds, drift := stringSetDelta(doc.MappedControls, remoteTargets[remote.ID])
		if len(adds) > 0 {
			plan.MappingAdds = append(plan.MappingAdds, MappingAdd{RefCode: doc.RefCode, ControlID: remote.ID, Targets: adds})
		}

		if len(drift) > 0 {
			plan.DriftMappings = append(plan.DriftMappings, MappingDrift{RefCode: doc.RefCode, Targets: drift})
		}
	}

	for _, rc := range state.Controls {
		if _, ok := consumed[rc.ID]; !ok {
			plan.DriftControls = append(plan.DriftControls, rc)
		}
	}
}

// planPolicies matches manifest entries against remote policies
func planPolicies(plan *Plan, ws *Workspace, state *RemoteState) error {
	idOf := func(p RemotePolicy) string { return p.ID }
	byID := lo.SliceToMap(state.Policies, func(p RemotePolicy) (string, RemotePolicy) { return p.ID, p })
	byName := lo.SliceToMap(state.Policies, func(p RemotePolicy) (string, RemotePolicy) {
		return strings.ToLower(p.Name), p
	})

	consumed := map[string]struct{}{}

	for _, policy := range ws.Policies {
		markdown, body, err := ws.policyMarkdown(policy)
		if err != nil {
			return err
		}

		remote, ok := matchRemote(policy.ID, policy.Name, byID, byName, consumed, idOf)
		if !ok {
			plan.CreatePolicies = append(plan.CreatePolicies, PolicyCreate{Policy: policy, Markdown: markdown})

			continue
		}

		consumed[remote.ID] = struct{}{}

		update := PolicyUpdate{Policy: policy, Remote: remote, Markdown: markdown}

		if policy.Name != remote.Name {
			update.Diffs = append(update.Diffs, FieldDiff{Field: "name", Old: remote.Name, New: policy.Name})
		}

		if policy.PolicyType != "" && policy.PolicyType != lo.FromPtr(remote.KindName) {
			update.Diffs = append(update.Diffs, FieldDiff{Field: "policyType", Old: lo.FromPtr(remote.KindName), New: policy.PolicyType})
		}

		if diff, changed := tagsDiff(remote.Tags, policy.Tags); changed {
			update.Diffs = append(update.Diffs, diff)
		}

		if sanitizedBody(body) != sanitizedBody(lo.FromPtr(remote.Details)) {
			update.BodyChanged = true
		}

		remoteControls := lo.Map(remote.Controls, func(r RemoteRef, _ int) string { return r.RefCode })

		adds, drift := stringSetDelta(policy.MappedControls, remoteControls)
		update.AddControls = adds

		if len(drift) > 0 {
			plan.DriftPolicyControls = append(plan.DriftPolicyControls, PolicyDrift{Name: policy.Name, Targets: drift})
		}

		if len(update.Diffs) > 0 || len(update.AddControls) > 0 || update.BodyChanged {
			plan.UpdatePolicies = append(plan.UpdatePolicies, update)
		}
	}

	for _, rp := range state.Policies {
		if _, ok := consumed[rp.ID]; !ok {
			plan.DriftPolicies = append(plan.DriftPolicies, rp)
		}
	}

	return nil
}

// controlDiffs computes the field-level differences an inventory entry would
// apply to its matched control, empty entry values are unmanaged and never
// diff nor clear the API field
func controlDiffs(doc *controlfile.Control, rc RemoteControl) []FieldDiff {
	var diffs []FieldDiff

	add := func(field, old, desired string) {
		if desired != "" && old != desired {
			diffs = append(diffs, FieldDiff{Field: field, Old: old, New: desired})
		}
	}

	add("refCode", rc.RefCode, doc.RefCode)
	add("title", rc.Title, doc.Title)
	add("description", rc.Description, doc.Description)
	add("category", rc.Category, doc.Category)
	add("subcategory", rc.Subcategory, doc.Subcategory)

	if diff, changed := tagsDiff(rc.Tags, doc.Tags); changed {
		diffs = append(diffs, diff)
	}

	return diffs
}

// tagsDiff compares tag sets order-insensitively, an empty workspace list is
// unmanaged and never diffs, the data itself is never reordered
func tagsDiff(remote, desired []string) (FieldDiff, bool) {
	if len(desired) == 0 {
		return FieldDiff{}, false
	}

	sortedRemote := slices.Clone(remote)
	slices.Sort(sortedRemote)

	sortedDesired := slices.Clone(desired)
	slices.Sort(sortedDesired)

	if slices.Equal(sortedRemote, sortedDesired) {
		return FieldDiff{}, false
	}

	return FieldDiff{Field: "tags", Old: strings.Join(remote, ", "), New: strings.Join(desired, ", ")}, true
}

// remoteMappedTargets derives the mapped refCode list per control ID from the
// remote mappings, using the same directional rule as BuildControls
func remoteMappedTargets(state *RemoteState) map[string][]string {
	targets := map[string][]string{}

	for _, mapping := range state.Mappings {
		codes := lo.Map(mapping.To, func(r RemoteRef, _ int) string { return r.RefCode })

		for _, from := range mapping.From {
			targets[from.ID] = append(targets[from.ID], codes...)
		}
	}

	return targets
}

// sanitizedBody normalizes a document body the way the server does on import
func sanitizedBody(body string) string {
	return strings.TrimSpace(sanitizer.Sanitize(body))
}

// stringSetDelta compares desired and current values case-insensitively and
// returns the additions and the current values missing from desired
func stringSetDelta(desired, current []string) (adds, missing []string) {
	currentSet := lo.SliceToMap(current, func(v string) (string, struct{}) { return strings.ToLower(v), struct{}{} })
	desiredSet := lo.SliceToMap(desired, func(v string) (string, struct{}) { return strings.ToLower(v), struct{}{} })

	adds = lo.Uniq(lo.Filter(desired, func(v string, _ int) bool {
		_, ok := currentSet[strings.ToLower(v)]

		return !ok
	}))

	missing = lo.Uniq(lo.Filter(current, func(v string, _ int) bool {
		_, ok := desiredSet[strings.ToLower(v)]

		return !ok
	}))

	return adds, missing
}
