package engine

import (
	"html"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// An empty field is unmanaged: courier leaves the value in Openlane alone
// rather than clearing it, so only fields the file actually sets are compared.
// Remote values are normalized the same way pull renders them, so a record
// written by pull compares equal to its file until someone edits it

// changedString reports whether a managed string differs from the remote
// value, an unset field is unmanaged and never counts as a change
func changedString(doc, remote string) bool {
	return doc != "" && doc != remote
}

// changedStringPtr is changedString against a remote value the API leaves unset
func changedStringPtr(doc string, remote *string) bool {
	return changedString(doc, lo.FromPtr(remote))
}

// changedFold is changedString for values that resolve case-insensitively, so
// casing alone is not an edit
func changedFold(doc, remote string) bool {
	return doc != "" && !strings.EqualFold(doc, remote)
}

// changedSlice reports whether a managed list differs from the remote list,
// order is not significant and an empty list is unmanaged
func changedSlice(doc, remote []string) bool {
	return len(doc) > 0 && !sameValues(doc, remote)
}

// FieldDiff is one managed field that differs, carrying the value in Openlane
// and the value the file would write, so a report can show the edit itself
type FieldDiff struct {
	// Field is the managed field name, as the file spells it
	Field string `json:"field"`
	// From is the value in Openlane, empty when the record does not set it
	From string `json:"from,omitempty"`
	// To is the value the file carries
	To string `json:"to,omitempty"`
}

// fieldChanges collects the managed fields that differ, in the order compared
type fieldChanges []FieldDiff

// record adds a field whose values differ
func (f *fieldChanges) record(field, from, to string) {
	*f = append(*f, FieldDiff{Field: field, From: from, To: to})
}

// str compares a managed string
func (f *fieldChanges) str(field, doc, remote string) {
	if changedString(doc, remote) {
		f.record(field, remote, doc)
	}
}

// strPtr compares a managed string against a remote value the API leaves unset
func (f *fieldChanges) strPtr(field, doc string, remote *string) {
	if changedStringPtr(doc, remote) {
		f.record(field, lo.FromPtr(remote), doc)
	}
}

// fold compares a value that resolves case-insensitively
func (f *fieldChanges) fold(field, doc, remote string) {
	if changedFold(doc, remote) {
		f.record(field, remote, doc)
	}
}

// slice compares a managed list, order is not significant
func (f *fieldChanges) slice(field string, doc, remote []string) {
	if changedSlice(doc, remote) {
		f.record(field, strings.Join(remote, ", "), strings.Join(doc, ", "))
	}
}

// names lists the changed field names, the form the status gate and the
// terminal output read
func (f fieldChanges) names() []string {
	return lo.Map(f, func(d FieldDiff, _ int) string { return d.Field })
}

// changedControlFields names the managed fields of a control that differ from
// the record in Openlane, in file order
func changedControlFields(doc *controlfile.Control, remote RemoteControl) fieldChanges {
	var changed fieldChanges

	// only meaningful when the entry matched by ID, a refCode change on an
	// entry without one reads as a new control and is created instead
	if doc.ID != "" && doc.RefCode != remote.RefCode {
		changed.record("refCode", remote.RefCode, doc.RefCode)
	}

	changed.str("title", doc.Title, remote.Title)
	changed.str("description", doc.Description, plainText(remote.Description))
	changed.str("category", doc.Category, remote.Category)
	changed.str("subcategory", doc.Subcategory, remote.Subcategory)

	// statuses resolve case-insensitively, as the enum parses them
	changed.fold("status", doc.Status, remote.Status)

	changed.str("categoryID", doc.CategoryID, remote.CategoryID)

	// group display names resolve case-insensitively, so casing alone is not an edit
	changed.fold("controlOwner", doc.ControlOwner, remote.ControlOwner)
	changed.fold("delegate", doc.Delegate, remote.Delegate)

	changed.str("referenceID", doc.ReferenceID, remote.ReferenceID)
	changed.str("auditorReferenceID", doc.AuditorReferenceID, remote.AuditorReferenceID)
	changed.slice("tags", doc.Tags, remote.Tags)

	return changed
}

// groupNamesByID indexes group display names by their Openlane ULID, so the
// owner and delegate of a control render as the names the file carries
func groupNamesByID(groups []RemoteGroup) map[string]string {
	return lo.SliceToMap(groups, func(g RemoteGroup) (string, string) {
		return g.ID, g.DisplayName
	})
}

// groupIDsByName indexes group IDs by lowercased display name, the file names
// the group and apply resolves it the way refCodes resolve, case-insensitively
func groupIDsByName(groups []RemoteGroup) map[string]string {
	return lo.SliceToMap(groups, func(g RemoteGroup) (string, string) {
		return strings.ToLower(g.DisplayName), g.ID
	})
}

// The server parses the uploaded document and its frontmatter overrides the
// mutation input, so for fields the document also carries the frontmatter is
// what actually lands. Comparing and sending the manifest value instead would
// drop a frontmatter edit silently

// effectiveName is the policy name the server will apply
func effectiveName(policy *controlfile.Policy, fm controlfile.Frontmatter) string {
	if fm.Title != "" {
		return fm.Title
	}

	return policy.Name
}

// effectiveTags are the tags the server will apply
func effectiveTags(policy *controlfile.Policy, fm controlfile.Frontmatter) []string {
	if len(fm.Tags) > 0 {
		return fm.Tags
	}

	return policy.Tags
}

// changedPolicyFields names the managed fields and the body of a policy that
// differ from the record in Openlane, in document order
func changedPolicyFields(policy *controlfile.Policy, fm controlfile.Frontmatter, body string, remote RemotePolicy) fieldChanges {
	var changed fieldChanges

	changed.str("name", effectiveName(policy, fm), remote.Name)
	changed.strPtr("policyType", policy.PolicyType, remote.KindName)
	changed.fold("status", fm.Status, remote.Status)

	// revision is not compared: the server bumps it after every write, so the
	// file is stale by one the moment an apply lands. Treating that as an edit
	// makes every apply write again and bump again, which never converges. The
	// file's revision still goes out with a real change, it just cannot be the
	// thing that triggers one

	changed.slice("tags", effectiveTags(policy, fm), remote.Tags)

	if remoteBody := renderBody(remote.Details); body != remoteBody {
		changed.record("body", remoteBody, body)
	}

	return changed
}

// importedSource marks the mappings courier created and may edit in place,
// standing in for the systemInternalID key harmonize uses, which the API
// restricts to system admins
const importedSource = "IMPORTED"

// ownedMapping is a mapping courier created for one control and framework
type ownedMapping struct {
	// id is the Openlane ULID of the mapped control record
	id string
	// targets are the reference codes already on its to side
	targets []string
}

// ownedMappings indexes the mappings courier owns by control ID and framework,
// so added references extend the existing record instead of accumulating a new
// one per apply. Records whose targets span frameworks are left out, courier
// only ever writes one framework per record so those came from elsewhere
func ownedMappings(state *RemoteState) map[string]ownedMapping {
	owned := map[string]ownedMapping{}

	for _, mapping := range state.Mappings {
		if mapping.Source != importedSource || len(mapping.To) == 0 {
			continue
		}

		frameworks := lo.Uniq(lo.Map(mapping.To, func(ref RemoteRef, _ int) string { return frameworkOf(ref) }))
		if len(frameworks) != 1 {
			continue
		}

		targets := lo.Map(mapping.To, func(ref RemoteRef, _ int) string { return ref.RefCode })

		for _, from := range mapping.From {
			owned[mappingKey(from.ID, frameworks[0])] = ownedMapping{id: mapping.ID, targets: targets}
		}
	}

	return owned
}

// mappingKey identifies the mapping courier owns for a control and framework
func mappingKey(controlID, framework string) string {
	return controlID + "::" + framework
}

// frameworkOf is the framework a reference groups under, references without
// one belong to the organization's own controls
func frameworkOf(ref RemoteRef) string {
	if ref.Framework == "" {
		return controlfile.CustomFrameworkKey
	}

	return ref.Framework
}

// flattenTargets renders framework-grouped references as sorted
// "framework: refCode" strings for reporting
func flattenTargets(targets controlfile.MappedControls) []string {
	frameworks := lo.Keys(targets)
	slices.Sort(frameworks)

	var out []string

	for _, framework := range frameworks {
		codes := slices.Clone(targets[framework])
		slices.Sort(codes)

		for _, code := range codes {
			out = append(out, framework+": "+code)
		}
	}

	return out
}

// renderBody renders a stored policy body the same way pull writes it, so the
// result compares directly against the body in the store document
func renderBody(details *string) string {
	return html.UnescapeString(bodyToMarkdown(lo.FromPtr(details)))
}

// linkedControls groups the controls already linked to a policy
func linkedControls(remote RemotePolicy) controlfile.MappedControls {
	linked := controlfile.MappedControls{}
	addGroupedRefs(linked, remote.Controls)

	return linked
}

// missingTargets returns the references in desired that are not already
// present in existing, refCodes match case-insensitively as they resolve
func missingTargets(desired, existing controlfile.MappedControls) controlfile.MappedControls {
	missing := controlfile.MappedControls{}

	for framework, codes := range desired {
		for _, code := range codes {
			if slices.ContainsFunc(existing[framework], func(have string) bool { return strings.EqualFold(have, code) }) {
				continue
			}

			missing[framework] = append(missing[framework], code)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return missing
}

// sameValues reports whether two lists hold the same values, order is not significant
func sameValues(a, b []string) bool {
	left, right := lo.Difference(a, b)

	return len(left) == 0 && len(right) == 0
}
