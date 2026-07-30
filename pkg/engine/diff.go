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

// changedControlFields names the managed fields of a control that differ from
// the record in Openlane, in file order
func changedControlFields(doc *controlfile.Control, remote RemoteControl) []string {
	var changed []string

	// only meaningful when the entry matched by ID, a refCode change on an
	// entry without one reads as a new control and is created instead
	if doc.ID != "" && doc.RefCode != remote.RefCode {
		changed = append(changed, "refCode")
	}

	if doc.Title != "" && doc.Title != remote.Title {
		changed = append(changed, "title")
	}

	if doc.Description != "" && doc.Description != plainText(remote.Description) {
		changed = append(changed, "description")
	}

	if doc.Category != "" && doc.Category != remote.Category {
		changed = append(changed, "category")
	}

	if doc.Subcategory != "" && doc.Subcategory != remote.Subcategory {
		changed = append(changed, "subcategory")
	}

	if len(doc.Tags) > 0 && !sameValues(doc.Tags, remote.Tags) {
		changed = append(changed, "tags")
	}

	return changed
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
func changedPolicyFields(policy *controlfile.Policy, fm controlfile.Frontmatter, body string, remote RemotePolicy) []string {
	var changed []string

	if name := effectiveName(policy, fm); name != "" && name != remote.Name {
		changed = append(changed, "name")
	}

	if policy.PolicyType != "" && policy.PolicyType != lo.FromPtr(remote.KindName) {
		changed = append(changed, "policyType")
	}

	if fm.Status != "" && !strings.EqualFold(fm.Status, remote.Status) {
		changed = append(changed, "status")
	}

	// revision is not compared: the server bumps it after every write, so the
	// file is stale by one the moment an apply lands. Treating that as an edit
	// makes every apply write again and bump again, which never converges. The
	// file's revision still goes out with a real change, it just cannot be the
	// thing that triggers one

	if tags := effectiveTags(policy, fm); len(tags) > 0 && !sameValues(tags, remote.Tags) {
		changed = append(changed, "tags")
	}

	if body != renderBody(remote.Details) {
		changed = append(changed, "body")
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
