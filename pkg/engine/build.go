package engine

import (
	"html"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// buildControls converts remote state into the control inventory verbatim,
// the mappedControls of a control are the references on the to side of every
// mapping the control appears on the from side of, grouped by framework
func buildControls(state *RemoteState) []*controlfile.Control {
	targetsByControl := map[string]controlfile.MappedControls{}

	for _, mapping := range state.Mappings {
		for _, from := range mapping.From {
			if targetsByControl[from.ID] == nil {
				targetsByControl[from.ID] = controlfile.MappedControls{}
			}

			addGroupedRefs(targetsByControl[from.ID], mapping.To)
		}
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
			MarkdownPath: controlfile.PolicyMarkdownPath(rp.Name),
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

// addGroupedRefs adds remote references into a framework-grouped map,
// references without a framework group under the custom key
func addGroupedRefs(grouped controlfile.MappedControls, refs []RemoteRef) {
	for _, ref := range refs {
		key := ref.Framework
		if key == "" {
			key = controlfile.CustomFrameworkKey
		}

		grouped[key] = lo.Uniq(append(grouped[key], ref.RefCode))
	}
}

// buildControlsKind renders controls.yaml for the controls kind
func buildControlsKind(state *RemoteState) (kindFiles, error) {
	data, err := controlfile.Marshal(buildControls(state))
	if err != nil {
		return kindFiles{}, err
	}

	return kindFiles{files: map[string][]byte{controlfile.ControlsFile: data}}, nil
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
