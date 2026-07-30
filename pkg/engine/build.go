package engine

import (
	"fmt"
	"html"
	"slices"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// buildControls converts remote state into the control inventory verbatim,
// the mappedControls of a control are the references on the to side of every
// mapping the control appears on the from side of, grouped by framework
func buildControls(state *RemoteState) []*controlfile.Control {
	targetsByControl := mappedTargets(state)

	// subcontrols nest under the control they belong to, ones whose parent was
	// not exported belong to a framework control and are not authorable here
	subcontrols := map[string][]*controlfile.Subcontrol{}

	for _, rs := range state.Subcontrols {
		subcontrols[rs.ControlID] = append(subcontrols[rs.ControlID], &controlfile.Subcontrol{
			ID:             rs.ID,
			RefCode:        rs.RefCode,
			Title:          rs.Title,
			Description:    plainText(rs.Description),
			Category:       rs.Category,
			Subcategory:    rs.Subcategory,
			Tags:           rs.Tags,
			MappedControls: targetsByControl[rs.ID],
		})
	}

	return lo.Map(state.Controls, func(rc RemoteControl, _ int) *controlfile.Control {
		return &controlfile.Control{
			ID:             rc.ID,
			RefCode:        rc.RefCode,
			Title:          rc.Title,
			Description:    plainText(rc.Description),
			Category:       rc.Category,
			Subcategory:    rc.Subcategory,
			Tags:           rc.Tags,
			MappedControls: targetsByControl[rc.ID],
			Subcontrols:    subcontrols[rc.ID],
		}
	})
}

// buildPolicies converts remote policies into the manifest plus one markdown
// document per policy, keyed by store-relative path. Bodies are rendered
// from the stored server content, markdown that courier uploaded round-trips
// verbatim and rich-text edits made in the UI arrive as converted markdown
func buildPolicies(state *RemoteState) ([]*controlfile.Policy, map[string][]byte, error) {
	policies := make([]*controlfile.Policy, 0, len(state.Policies))
	markdown := map[string][]byte{}
	paths := documentPaths(state.Policies)

	for _, rp := range state.Policies {
		linked := controlfile.MappedControls{}
		addGroupedRefs(linked, rp.Controls)

		if len(linked) == 0 {
			linked = nil
		}

		policy := &controlfile.Policy{
			ID:           rp.ID,
			Name:         rp.Name,
			PolicyType:   lo.FromPtr(rp.KindName),
			MarkdownPath: paths[rp.ID],
			Tags:         rp.Tags,
		}

		body := html.UnescapeString(bodyToMarkdown(lo.FromPtr(rp.Details)))

		doc, err := controlfile.MarshalPolicyMarkdown(controlfile.Frontmatter{
			OpenlaneID: rp.ID,
			Title:      rp.Name,
			Status:     rp.Status,
			Tags:       rp.Tags,
			Revision:   rp.Revision,
			Satisfies:  linked,
		}, body)
		if err != nil {
			return nil, nil, err
		}

		markdown[policy.MarkdownPath] = doc
		policies = append(policies, policy)
	}

	return policies, markdown, nil
}

// mappedTargets groups the references already mapped from each control by
// control ID, the targets are the to side of every mapping the control
// appears on the from side of
func mappedTargets(state *RemoteState) map[string]controlfile.MappedControls {
	targets := map[string]controlfile.MappedControls{}

	for _, mapping := range state.Mappings {
		for _, from := range mapping.From {
			if targets[from.ID] == nil {
				targets[from.ID] = controlfile.MappedControls{}
			}

			addGroupedRefs(targets[from.ID], mapping.To)
		}
	}

	return targets
}

// documentPaths assigns a markdown path to every policy by ID. Names that
// reduce to the same file name are all disambiguated by their ID, so the
// assignment does not depend on the order the API returned
func documentPaths(policies []RemotePolicy) map[string]string {
	counts := map[string]int{}

	for _, rp := range policies {
		counts[controlfile.PolicyMarkdownPath(rp.Name)]++
	}

	paths := make(map[string]string, len(policies))

	for _, rp := range policies {
		path := controlfile.PolicyMarkdownPath(rp.Name)
		if counts[path] > 1 {
			path = controlfile.PolicyMarkdownPathWithID(rp.Name, rp.ID)
		}

		paths[rp.ID] = path
	}

	return paths
}

// addGroupedRefs adds remote references into a framework-grouped map,
// references without a framework group under the custom key. Controls and
// subcontrols share the list, the file names a reference and apply resolves
// which of the two it is
func addGroupedRefs(grouped controlfile.MappedControls, refs []RemoteRef) {
	for _, ref := range refs {
		key := frameworkOf(ref)

		grouped[key] = lo.Uniq(append(grouped[key], ref.RefCode))
	}
}

// buildControlsKind renders controls.yaml for the controls kind
func buildControlsKind(state *RemoteState) (kindFiles, error) {
	data, err := controlfile.Marshal(buildControls(state))
	if err != nil {
		return kindFiles{}, err
	}

	return kindFiles{
		files:    map[string][]byte{controlfile.ControlsFile: data},
		warnings: orphanSubcontrols(state),
	}, nil
}

// orphanSubcontrols reports subcontrols the export cannot place, they hang off
// a control that is not itself exported, so nesting them would lose the parent
func orphanSubcontrols(state *RemoteState) []string {
	exported := lo.SliceToMap(state.Controls, func(rc RemoteControl) (string, struct{}) {
		return rc.ID, struct{}{}
	})

	var orphans []string

	for _, rs := range state.Subcontrols {
		if _, ok := exported[rs.ControlID]; !ok {
			orphans = append(orphans, fmt.Sprintf(
				"subcontrol %q not exported, its parent control is not an organization control", rs.RefCode))
		}
	}

	slices.Sort(orphans)

	return orphans
}

// buildPoliciesKind renders policies.yaml and the policy documents for the
// policies kind, stale documents are removed on write
func buildPoliciesKind(state *RemoteState) (kindFiles, error) {
	policies, markdown, err := buildPolicies(state)
	if err != nil {
		return kindFiles{}, err
	}

	data, err := controlfile.Marshal(policies)
	if err != nil {
		return kindFiles{}, err
	}

	files := map[string][]byte{controlfile.PoliciesFile: data}
	for path, doc := range markdown {
		files[path] = doc
	}

	return kindFiles{files: files, cleanup: removeStaleMarkdown}, nil
}
